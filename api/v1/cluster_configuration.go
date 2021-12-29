/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	"sort"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// CreatePostgresqlConfiguration creates the PostgreSQL configuration to be
// used for this cluster and return it and its sha256 checksum
func (cluster *Cluster) CreatePostgresqlConfiguration() (string, string, error) {
	// Extract the PostgreSQL major version
	fromVersion, err := cluster.GetPostgresqlVersion()
	if err != nil {
		return "", "", err
	}

	info := postgres.ConfigurationInfo{
		Settings:                         postgres.CnpConfigurationSettings,
		MajorVersion:                     fromVersion,
		UserSettings:                     cluster.Spec.PostgresConfiguration.Parameters,
		IncludingMandatory:               true,
		IncludingSharedPreloadLibraries:  true,
		AdditionalSharedPreloadLibraries: cluster.Spec.PostgresConfiguration.AdditionalLibraries,
		IsReplicaCluster:                 cluster.IsReplica(),
	}

	// We need to include every replica inside the list of possible synchronous standbys
	info.Replicas = nil
	for _, instances := range cluster.Status.InstancesStatus {
		for _, instance := range instances {
			if cluster.Status.CurrentPrimary != instance {
				info.Replicas = append(info.Replicas, instance)
			}
		}
	}

	// Ensure a consistent ordering to avoid spurious configuration changes
	sort.Strings(info.Replicas)

	// Compute the actual number of sync replicas
	info.SyncReplicas = cluster.GetSyncReplicasNumber()

	// Set cluster name
	info.ClusterName = cluster.Name

	conf, sha256 := postgres.CreatePostgresqlConfFile(postgres.CreatePostgresqlConfiguration(info))
	return conf, sha256, nil
}

// GetSyncReplicasNumber computes the actual number of required synchronous replicas
// given the requested min, max and the number of ready replicas in the cluster
func (cluster *Cluster) GetSyncReplicasNumber() (syncReplicas int) {
	// We start with the number of healthy replicas (healthy pods minus one)
	// and verify it is greater than 0 and between minSyncReplicas and maxSyncReplicas.
	// Formula: 1 <= minSyncReplicas <= SyncReplicas <= maxSyncReplicas < readyReplicas
	readyReplicas := len(cluster.Status.InstancesStatus[utils.PodHealthy]) - 1

	// Initially set it to the max sync replicas requested by user
	syncReplicas = int(cluster.Spec.MaxSyncReplicas)

	// Lower to min sync replicas if not enough ready replicas
	if readyReplicas < syncReplicas {
		syncReplicas = int(cluster.Spec.MinSyncReplicas)
	}

	// Lower to ready replicas if min sync replicas is too high
	// (this is a self-healing procedure that prevents from a
	// temporarily unresponsive system)
	if readyReplicas < int(cluster.Spec.MinSyncReplicas) {
		syncReplicas = readyReplicas
		log.Info("Ignore minSyncReplicas to enforce self-healing",
			"syncReplicas", readyReplicas,
			"minSyncReplicas", cluster.Spec.MinSyncReplicas,
			"maxSyncReplicas", cluster.Spec.MaxSyncReplicas)
	}

	return syncReplicas
}

// CreatePostgresqlHBA creates the HBA rules for this cluster
func (cluster *Cluster) CreatePostgresqlHBA() (string, error) {
	version, err := cluster.GetPostgresqlVersion()
	if err != nil {
		return "", err
	}

	// From PostgreSQL 14 we default to SCRAM-SHA-256
	// authentication as the default `password_encryption`
	// is set to `scram-sha-256` and this is the most
	// secure authentication method available.
	//
	// See:
	// https://www.postgresql.org/docs/14/release-14.html
	defaultAuthenticationMethod := "scram-sha-256"
	if version < 140000 {
		defaultAuthenticationMethod = "md5"
	}

	return postgres.CreateHBARules(
		cluster.Spec.PostgresConfiguration.PgHBA,
		defaultAuthenticationMethod)
}
