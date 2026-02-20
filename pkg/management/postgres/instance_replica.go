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

package postgres

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
)

// RefreshReplicaConfiguration writes the PostgreSQL correct
// replication configuration for connecting to the right primary server,
// depending on the cluster replica mode
func (instance *Instance) RefreshReplicaConfiguration(
	ctx context.Context,
	cluster *apiv1.Cluster,
	cli client.Client,
) (changed bool, err error) {
	// TODO: Remove this code when enough time has passed since 1.21 release
	//       This is due to the operator switching from postgresql.auto.conf
	//       to override.conf for coordinating replication configuration
	changed, err = instance.migratePostgresAutoConfFile(ctx)
	if err != nil {
		return changed, err
	}

	primary, err := instance.IsPrimary()
	if err != nil {
		return changed, err
	}

	if primary && !instance.RequiresDesignatedPrimaryTransition() {
		return changed, nil
	}

	if cluster.IsReplica() && cluster.Status.TargetPrimary == instance.GetPodName() {
		result, err := instance.writeReplicaConfigurationForDesignatedPrimary(ctx, cli, cluster)
		return changed || result, err
	}
	result, err := instance.writeReplicaConfigurationForReplica(cluster)
	return changed || result, err
}

func (instance *Instance) writeReplicaConfigurationForReplica(cluster *apiv1.Cluster) (changed bool, err error) {
	slotName := cluster.GetSlotNameFromInstanceName(instance.GetPodName())
	primaryConnInfo := instance.GetPrimaryConnInfo()
	return UpdateReplicaConfiguration(instance.PgData, primaryConnInfo, slotName)
}

func (instance *Instance) writeReplicaConfigurationForDesignatedPrimary(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
) (changed bool, err error) {
	server, ok := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)
	if !ok {
		return false, fmt.Errorf("missing external cluster")
	}

	connectionString, err := external.ConfigureConnectionToServer(
		ctx, cli, instance.GetNamespaceName(), &server)
	if err != nil {
		return false, err
	}

	return UpdateReplicaConfiguration(instance.PgData, connectionString, "")
}
