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

package controller

import (
	"context"
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// refreshReplicaConfiguration writes the PostgreSQL correct
// replication configuration for connecting to the right primary server,
// depending on the cluster replica mode
func (r *InstanceReconciler) refreshReplicaConfiguration(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (changed bool, err error) {
	// The "archive_mode" setting was used to be overridden in the "postgresql.auto.conf"
	// if the server was a designated primary. We need make sure to remove it
	// and fall back on the value defined in "custom.conf".
	// TODO: Removed this code together the RemoveArchiveModeFromPostgresAutoConf function
	// TODO: when enough time passed since 1.12 release
	changed, err = postgres.RemoveArchiveModeFromPostgresAutoConf(r.instance.PgData)
	if err != nil {
		return changed, err
	}

	primary, err := r.instance.IsPrimary()
	if err != nil {
		return false, err
	}

	if primary {
		return false, nil
	}

	if cluster.IsReplica() && cluster.Status.TargetPrimary == r.instance.PodName {
		return r.writeReplicaConfigurationForDesignatedPrimary(ctx, cluster)
	}

	return r.writeReplicaConfigurationForReplica()
}

func (r *InstanceReconciler) writeReplicaConfigurationForReplica() (changed bool, err error) {
	return postgres.UpdateReplicaConfiguration(r.instance.PgData, r.instance.ClusterName, r.instance.PodName)
}

func (r *InstanceReconciler) writeReplicaConfigurationForDesignatedPrimary(
	ctx context.Context, cluster *apiv1.Cluster,
) (changed bool, err error) {
	server, ok := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)
	if !ok {
		return false, fmt.Errorf("missing external cluster")
	}

	connectionString, pgpassfile, err := external.ConfigureConnectionToServer(
		ctx, r.client, r.instance.Namespace, &server)
	if err != nil {
		return false, err
	}

	if pgpassfile != "" {
		connectionString = fmt.Sprintf("%v passfile=%v",
			connectionString,
			pgpassfile)
	}

	return postgres.UpdateReplicaConfigurationForPrimary(r.instance.PgData, connectionString)
}
