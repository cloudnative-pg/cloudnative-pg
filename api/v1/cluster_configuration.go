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
	imageName := cluster.GetImageName()
	tag := utils.GetImageTag(imageName)
	fromVersion, err := postgres.GetPostgresVersionFromTag(tag)
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

	// We start with the number of healthy replicas (healthy pods minus one)
	// and verify it is greater than 0 and between minSyncReplicas and maxSyncReplicas.
	// Formula: 1 <= minSyncReplicas <= SyncReplicas <= maxSyncReplicas < readyReplicas
	readyReplicas := len(cluster.Status.InstancesStatus[utils.PodHealthy]) - 1

	// Initially set it to the max sync replicas requested by user
	info.SyncReplicas = int(cluster.Spec.MaxSyncReplicas)

	// Lower to min sync replicas if not enough ready replicas
	if readyReplicas < info.SyncReplicas {
		info.SyncReplicas = int(cluster.Spec.MinSyncReplicas)
	}

	// Lower to ready replicas if min sync replicas is too high
	// (this is a self-healing procedure that prevents from a
	// temporarily unresponsive system)
	if readyReplicas < int(cluster.Spec.MinSyncReplicas) {
		info.SyncReplicas = readyReplicas
		log.Log.Info("Ignore minSyncReplicas to enforce self-healing",
			"syncReplicas", readyReplicas,
			"minSyncReplicas", cluster.Spec.MinSyncReplicas,
			"maxSyncReplicas", cluster.Spec.MaxSyncReplicas)
	}

	// Set cluster name
	info.ClusterName = cluster.Name

	conf, sha256 := postgres.CreatePostgresqlConfFile(postgres.CreatePostgresqlConfiguration(info))
	return conf, sha256, nil
}

// CreatePostgresqlHBA creates the HBA rules for this cluster
func (cluster *Cluster) CreatePostgresqlHBA() string {
	return postgres.CreateHBARules(cluster.Spec.PostgresConfiguration.PgHBA)
}
