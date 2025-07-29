/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package replication

import (
	"context"
	"sort"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// legacySynchronousStandbyNames sets the standby node list with the
// legacy API
func legacySynchronousStandbyNames(ctx context.Context, cluster *apiv1.Cluster) postgres.SynchronousStandbyNamesConfig {
	syncReplicas, syncReplicasElectable := getSyncReplicasData(ctx, cluster)

	if syncReplicasElectable != nil && syncReplicas > 0 {
		escapedReplicas := make([]string, len(syncReplicasElectable))
		for idx, name := range syncReplicasElectable {
			escapedReplicas[idx] = escapePostgresConfLiteral(name)
		}
		return postgres.SynchronousStandbyNamesConfig{
			Method:       "ANY",
			NumSync:      syncReplicas,
			StandbyNames: syncReplicasElectable,
		}
	}

	return postgres.SynchronousStandbyNamesConfig{}
}

// getSyncReplicasData computes the actual number of required synchronous replicas and the names of
// the electable sync replicas given the requested min, max, the number of ready replicas in the cluster and the sync
// replicas constraints (if any)
func getSyncReplicasData(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (syncReplicas int, electableSyncReplicas []string) {
	contextLogger := log.FromContext(ctx)

	// We start with the number of healthy replicas (healthy pods minus one)
	// and verify it is greater than 0 and between minSyncReplicas and maxSyncReplicas.
	// Formula: 1 <= minSyncReplicas <= SyncReplicas <= maxSyncReplicas < readyReplicas
	readyReplicas := len(cluster.Status.InstancesStatus[apiv1.PodHealthy]) - 1

	// If the number of ready replicas is negative,
	// there are no healthy Pods so no sync replica can be configured
	if readyReplicas < 0 {
		return 0, nil
	}

	// Initially set it to the max sync replicas requested by user
	syncReplicas = cluster.Spec.MaxSyncReplicas

	// Lower to min sync replicas if not enough ready replicas
	if readyReplicas < syncReplicas {
		syncReplicas = cluster.Spec.MinSyncReplicas
	}

	// Lower to ready replicas if min sync replicas is too high
	// (this is a self-healing procedure that prevents from a
	// temporarily unresponsive system)
	if readyReplicas < cluster.Spec.MinSyncReplicas {
		syncReplicas = readyReplicas
		contextLogger.Warning("Ignore minSyncReplicas to enforce self-healing",
			"syncReplicas", readyReplicas,
			"minSyncReplicas", cluster.Spec.MinSyncReplicas,
			"maxSyncReplicas", cluster.Spec.MaxSyncReplicas)
	}

	electableSyncReplicas = getElectableSyncReplicas(ctx, cluster)
	numberOfElectableSyncReplicas := len(electableSyncReplicas)
	if numberOfElectableSyncReplicas < syncReplicas {
		contextLogger.Warning("lowering sync replicas due to not enough electable instances for sync replication "+
			"given the constraints",
			"electableSyncReplicasWithoutConstraints", syncReplicas,
			"electableSyncReplicasWithConstraints", numberOfElectableSyncReplicas,
			"constraints", cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint)
		syncReplicas = numberOfElectableSyncReplicas
	}

	return syncReplicas, electableSyncReplicas
}

// getElectableSyncReplicas computes the names of the instances that can be elected to sync replicas
func getElectableSyncReplicas(ctx context.Context, cluster *apiv1.Cluster) []string {
	contextLogger := log.FromContext(ctx)

	nonPrimaryInstances := getSortedNonPrimaryHealthyInstanceNames(cluster)

	topology := cluster.Status.Topology
	// We need to include every replica inside the list of possible synchronous standbys if we have no constraints
	// or the topology extraction is failing. This avoids a continuous operator crash.
	// One case this could happen is while draining nodes
	if !cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint.Enabled {
		return nonPrimaryInstances
	}

	// The same happens if we have failed to extract topology, we want to preserve the current status by adding all the
	// electable instances.
	if !topology.SuccessfullyExtracted {
		contextLogger.Warning("topology data not extracted, falling back to all electable sync replicas")
		return nonPrimaryInstances
	}

	currentPrimary := apiv1.PodName(cluster.Status.CurrentPrimary)
	// given that the constraints are based off the primary instance if we still don't have one we cannot continue
	if currentPrimary == "" {
		contextLogger.Warning("no primary elected, cannot compute electable sync replicas")
		return nil
	}

	currentPrimaryTopology, ok := topology.Instances[currentPrimary]
	if !ok {
		contextLogger.Warning("current primary topology not yet extracted, cannot computed electable sync replicas",
			"instanceName", currentPrimary)
		return nil
	}

	electableReplicas := make([]string, 0, len(nonPrimaryInstances))
	for _, name := range nonPrimaryInstances {
		name := apiv1.PodName(name)

		instanceTopology, ok := topology.Instances[name]
		// if we still don't have the topology data for the node we skip it from inserting it in the electable pool
		if !ok {
			contextLogger.Warning("current instance topology not found", "instanceName", name)
			continue
		}

		if !currentPrimaryTopology.MatchesTopology(instanceTopology) {
			electableReplicas = append(electableReplicas, string(name))
		}
	}

	return electableReplicas
}

func getSortedNonPrimaryHealthyInstanceNames(cluster *apiv1.Cluster) []string {
	var nonPrimaryInstances []string
	for _, instance := range cluster.Status.InstancesStatus[apiv1.PodHealthy] {
		if cluster.Status.CurrentPrimary != instance {
			nonPrimaryInstances = append(nonPrimaryInstances, instance)
		}
	}

	sort.Strings(nonPrimaryInstances)
	return nonPrimaryInstances
}
