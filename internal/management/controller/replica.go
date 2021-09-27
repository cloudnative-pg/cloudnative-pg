/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/external"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// RefreshReplicaConfiguration gets the latest cluster status from Kubernetes and then
// writes the correct replication configuration
func (r *InstanceReconciler) RefreshReplicaConfiguration(ctx context.Context) error {
	var cluster apiv1.Cluster
	err := r.client.Get(ctx, client.ObjectKey{Namespace: r.instance.Namespace, Name: r.instance.ClusterName}, &cluster)
	if err != nil {
		return err
	}

	_, err = r.WriteReplicaConfiguration(ctx, &cluster)
	return err
}

// WriteReplicaConfiguration writes the PostgreSQL replica configuration for connecting to the
// right primary server, depending on the cluster replica mode
func (r *InstanceReconciler) WriteReplicaConfiguration(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (changed bool, err error) {
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
	// The "archive_mode" setting could be set to "always" in the "postgresql.auto.conf"
	// if the server was a designated primary before. We need make sure to remove it
	// and fall back on the value defined in "custom.conf" that is always "on".
	changed, err = postgres.RemoveArchiveModeFromPostgresAutoConf(r.instance.PgData)
	if err != nil {
		return changed, err
	}

	return postgres.UpdateReplicaConfiguration(r.instance.PgData, r.instance.ClusterName, r.instance.PodName)
}

func (r *InstanceReconciler) writeReplicaConfigurationForDesignatedPrimary(
	ctx context.Context, cluster *apiv1.Cluster) (changed bool, err error) {
	// On designated primary, we want the "archive_mode" parameter set to "always",
	// so we can archive WAL files as if this instance would be a primary.
	changed, err = postgres.SetArchiveModeToAlwaysIntoPostgresAutoConf(r.instance.PgData)
	if err != nil {
		return changed, err
	}

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
