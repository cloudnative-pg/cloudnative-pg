/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package config

import (
	corev1 "k8s.io/api/core/v1"
)

// Secrets is the set of data that is needed to compute a PgBouncer configuration
type Secrets struct {
	// The secret containing the credentials to be used to execute the auth_query queries.
	AuthQuery *corev1.Secret

	// The TLS secret that will be used for client connections (application-side)
	Client *corev1.Secret

	// The root-CA that will be used to validate client certificates
	ClientCA *corev1.Secret

	// The CA that will be used to validate the connections to PostgreSQL
	ServerCA *corev1.Secret
}

// ConfigurationFiles is a set of configuration files that are needed for
// pgbouncer to work
type ConfigurationFiles map[string][]byte
