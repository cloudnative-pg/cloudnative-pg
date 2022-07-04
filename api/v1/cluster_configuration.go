/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetSyncReplicasData computes the actual number of required synchronous replicas and the names of
// the electable sync replicas given the requested min, max, the number of ready replicas in the cluster and the sync
// replicas constraints (if any)
func (cluster *Cluster) GetSyncReplicasData() (syncReplicas int, electableSyncReplicas []string) {
	// We start with the number of healthy replicas (healthy pods minus one)
	// and verify it is greater than 0 and between minSyncReplicas and maxSyncReplicas.
	// Formula: 1 <= minSyncReplicas <= SyncReplicas <= maxSyncReplicas < readyReplicas
	readyReplicas := len(cluster.Status.InstancesStatus[utils.PodHealthy]) - 1

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
		log.Warning("Ignore minSyncReplicas to enforce self-healing",
			"syncReplicas", readyReplicas,
			"minSyncReplicas", cluster.Spec.MinSyncReplicas,
			"maxSyncReplicas", cluster.Spec.MaxSyncReplicas)
	}

	electableSyncReplicas = cluster.getElectableSyncReplicas()
	numberOfElectableSyncReplicas := len(electableSyncReplicas)
	if numberOfElectableSyncReplicas < syncReplicas {
		log.Warning("lowering sync replicas due to not enough electable instances for sync replication "+
			"given the constraints",
			"electableSyncReplicasWithoutConstraints", syncReplicas,
			"electableSyncReplicasWithConstraints", numberOfElectableSyncReplicas,
			"constraints", cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint)
		syncReplicas = numberOfElectableSyncReplicas
	}

	return syncReplicas, electableSyncReplicas
}

// getElectableSyncReplicas computes the names of the instances that can be elected to sync replicas
func (cluster *Cluster) getElectableSyncReplicas() []string {
	var nonPrimaryInstances []string
	for _, instance := range cluster.Status.InstancesStatus[utils.PodHealthy] {
		if cluster.Status.CurrentPrimary != instance {
			nonPrimaryInstances = append(nonPrimaryInstances, instance)
		}
	}

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
		log.Warning("topology data not extracted, falling back to all electable sync replicas")
		return nonPrimaryInstances
	}

	currentPrimary := PodName(cluster.Status.CurrentPrimary)
	// given that the constraints are based off the primary instance if we still don't have one we cannot continue
	if currentPrimary == "" {
		log.Warning("no primary elected, cannot compute electable sync replicas")
		return nil
	}

	currentPrimaryTopology, ok := topology.Instances[currentPrimary]
	if !ok {
		log.Warning("current primary topology not yet extracted, cannot computed electable sync replicas",
			"instanceName", currentPrimary)
		return nil
	}

	electableReplicas := make([]string, 0, len(nonPrimaryInstances))
	for _, name := range nonPrimaryInstances {
		name := PodName(name)

		instanceTopology, ok := topology.Instances[name]
		// if we still don't have the topology data for the node we skip it from inserting it in the electable pool
		if !ok {
			log.Warning("current instance topology not found", "instanceName", name)
			continue
		}

		if !currentPrimaryTopology.matchesTopology(instanceTopology) {
			electableReplicas = append(electableReplicas, string(name))
		}
	}

	return electableReplicas
}
