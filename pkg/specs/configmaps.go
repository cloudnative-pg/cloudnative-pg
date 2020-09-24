/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"fmt"
	"math"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

const (
	// hbaHeader is the header of generated pg_hba.conf.
	// The content provided by the user is inserted after this text
	hbaHeader = `
# Grant local access
local all all peer
`

	// hbaFooter is the footer of generated pg_hba.conf.
	// The content provided by the user is inserted before this text
	hbaFooter = `
# Require md5 authentication elsewhere
host all all all md5
host replication all all md5
`
)

// MajorVersionRange is used to represent a range of PostgreSQL versions
type MajorVersionRange = struct {
	// The minimum limit of PostgreSQL major version, extreme included
	Min int

	// The maximum limit of PostgreSQL version, extreme excluded
	Max int
}

// PostgresSettings is a collection of PostgreSQL settings
type PostgresSettings = map[string]string

var (
	// globalDefaultPostgresSettings are the settings that are
	// applied to the PostgreSQL default configuration when
	// the user don't specify something different
	globalDefaultPostgresSettings = PostgresSettings{
		"max_parallel_workers":  "32",
		"max_worker_processes":  "32",
		"max_replication_slots": "32",
	}

	// defaultPostgresSettings are like globalDefeaultPostgresSettings
	// but are relative only to certain PostgreSQL versions
	defaultPostgresSettings = map[MajorVersionRange]PostgresSettings{
		{0, 130000}: {
			"wal_keep_segments": "32",
		},
		{130000, math.MaxInt64}: {
			"wal_keep_size": "512MB",
		},
	}

	// mandatoryPostgresSettings are the settings that are
	// applied to the final PostgreSQL configuration, even
	// if the user specified something different
	mandatoryPostgresSettings = PostgresSettings{
		"hot_standby":     "true",
		"archive_mode":    "on",
		"archive_command": "/controller/manager wal-archive %p",
	}
)

// CreatePostgresConfigMap create a configMap for this cluster
func CreatePostgresConfigMap(cluster *v1alpha1.Cluster) (*corev1.ConfigMap, error) {
	// put the user provided content between header and footer
	var hbaContent []string
	hbaContent = append(hbaContent, strings.TrimSpace(hbaHeader), "")
	if len(cluster.Spec.PostgresConfiguration.PgHBA) > 0 {
		hbaContent = append(hbaContent, cluster.Spec.PostgresConfiguration.PgHBA...)
		hbaContent = append(hbaContent, "")
	}
	hbaContent = append(hbaContent, strings.TrimSpace(hbaFooter))
	hbaContent = append(hbaContent, "")

	// Extract the PostgreSQL major version
	imageName := cluster.GetImageName()
	tag := utils.GetImageTag(imageName)
	fromVersion, err := postgres.GetPostgresVersionFromTag(tag)
	if err != nil {
		return nil, err
	}

	configFile := CreatePostgresqlConfFile(
		CreatePostgresqlConfiguration(
			fromVersion,
			cluster.Spec.PostgresConfiguration.Parameters))

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			"postgresConfiguration": configFile,
			"postgresHBA":           strings.Join(hbaContent, "\n"),
		},
	}, nil
}

// CreatePostgresqlConfiguration create the configuration from the settings
// and the default values
func CreatePostgresqlConfiguration(majorVersion int, parameters map[string]string) map[string]string {
	configuration := make(map[string]string)

	// start from the default settings
	for key, value := range globalDefaultPostgresSettings {
		configuration[key] = value
	}

	// apply settings relative to a certain PostgreSQL version
	for constraints, settings := range defaultPostgresSettings {
		if constraints.Min <= majorVersion && majorVersion < constraints.Max {
			for key, value := range settings {
				configuration[key] = value
			}
		}
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
