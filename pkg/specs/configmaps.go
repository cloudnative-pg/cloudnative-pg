/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
)

const (
	// defaultHbaContent is the default pg_hba.conf that is usei if the user
	// don't specify something different
	defaultHbaContent = `
# Grant local access
local all all peer
host all all 127.0.0.1/32 trust
host all all ::1/128 trust

# Require md5 authentication elsewhere
host all all all md5
host replication all all md5
`
)

var (
	// defaultPostgresSettings are the settings that are
	// applied to the PostgreSQL default configuration when
	// the user don't specify something different
	defaultPostgresSettings = map[string]string{
		"max_parallel_workers":  "32",
		"max_worker_processes":  "32",
		"max_replication_slots": "32",
		"wal_keep_segments":     "32",
	}

	// mandatoryPostgresSettings are the settings that are
	// applied to the final PostgreSQL configuration, even
	// if the user specified something different
	mandatoryPostgresSettings = map[string]string{
		"hot_standby":     "true",
		"archive_mode":    "on",
		"archive_command": "/controller/manager wal-archive %p",
	}
)

// CreatePostgresConfigMap create a configMap for this cluster
func CreatePostgresConfigMap(cluster *v1alpha1.Cluster) *corev1.ConfigMap {
	hbaContent := strings.Join(cluster.Spec.PostgresConfiguration.PgHBA, "\n")
	if hbaContent == "" {
		hbaContent = defaultHbaContent
	}

	configFile := CreatePostgresqlConfFile(
		CreatePostgresqlConfiguration(
			cluster.Spec.PostgresConfiguration.Parameters))

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			"postgresConfiguration": configFile,
			"postgresHBA":           hbaContent,
		},
	}
}

// CreatePostgresqlConfiguration create the configuration from the settings
// and the default values
func CreatePostgresqlConfiguration(parameters map[string]string) map[string]string {
	configuration := make(map[string]string)

	// start from the default settings
	for key, value := range defaultPostgresSettings {
		configuration[key] = value
	}

	// apply the values from the user
	for key, value := range parameters {
		configuration[key] = value
	}

	// apply the mandatory settings
	for key, value := range mandatoryPostgresSettings {
		configuration[key] = value
	}

	return configuration
}

// CreatePostgresqlConfFile create the contents of the postgresql.conf file
func CreatePostgresqlConfFile(parameters map[string]string) string {
	// create final configuration
	postgresConf := ""
	for key, value := range parameters {
		postgresConf += fmt.Sprintf("%v = %v\n", key, escapePostgresConfValue(value))
	}
	return postgresConf
}

// escapePostgresConfValue escapes a value to make its representation
// directly embeddable in the PostgreSQL configuration file
func escapePostgresConfValue(value string) string {
	return fmt.Sprintf("'%v'", strings.ReplaceAll(value, "'", "''"))
}
