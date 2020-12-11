/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

const (
	// PostgreSQLConfigurationKeyName is the name of the key
	// inside the ConfigMap which is containing the PostgreSQL
	// configuration
	PostgreSQLConfigurationKeyName = "postgresConfiguration"

	// PostgreSQLHBAKeyName is the name of the key
	// inside the ConfigMap which is containing the PostgreSQL
	// HBA rules
	PostgreSQLHBAKeyName = "postgresHBA"
)

// CreatePostgresConfigMap create a configMap for this cluster
func CreatePostgresConfigMap(cluster *v1alpha1.Cluster) (*corev1.ConfigMap, error) {
	// put the user provided content between header and footer
	hbaContent := postgres.CreateHBARules(cluster.Spec.PostgresConfiguration.PgHBA)

	// Extract the PostgreSQL major version
	imageName := cluster.GetImageName()
	tag := utils.GetImageTag(imageName)
	fromVersion, err := postgres.GetPostgresVersionFromTag(tag)
	if err != nil {
		return nil, err
	}

	info := postgres.ConfigurationInfo{
		Settings:           postgres.CnpConfigurationSettings,
		MajorVersion:       fromVersion,
		UserSettings:       cluster.Spec.PostgresConfiguration.Parameters,
		IncludingMandatory: true,
	}

	// We need to include every replica inside the list of possible synchronous standbys
	info.Replicas = nil
	for _, instances := range cluster.Status.InstancesStatus {
		info.Replicas = append(info.Replicas, instances...)
	}

	// Ensure a consistent ordering to avoid spurious configuration changes
	sort.Strings(info.Replicas)

	// We start with the number of healthy replicas (healthy pods minus one)
	// and verify it is between minSyncReplicas and maxSyncReplicas
	info.SyncReplicas = len(cluster.Status.InstancesStatus[utils.PodHealthy]) - 1
	if info.SyncReplicas > int(cluster.Spec.MaxSyncReplicas) {
		info.SyncReplicas = int(cluster.Spec.MaxSyncReplicas)
	}
	if info.SyncReplicas < int(cluster.Spec.MinSyncReplicas) {
		info.SyncReplicas = int(cluster.Spec.MinSyncReplicas)
	}

	configFile := postgres.CreatePostgresqlConfFile(postgres.CreatePostgresqlConfiguration(info))

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			PostgreSQLConfigurationKeyName: configFile,
			PostgreSQLHBAKeyName:           hbaContent,
		},
	}, nil
}
