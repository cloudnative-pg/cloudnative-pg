/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
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

	configFile := postgres.CreatePostgresqlConfFile(
		postgres.CreatePostgresqlConfiguration(
			postgres.CnpConfigurationSettings,
			fromVersion,
			cluster.Spec.PostgresConfiguration.Parameters,
			true))

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
