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

package config

import (
	corev1 "k8s.io/api/core/v1"
)

// Secrets is the set of data that is needed to compute a PgBouncer configuration
type Secrets struct {
	// The secret containing the credentials to be used to execute the auth_query queries.
	AuthQuery *corev1.Secret

	// The secret containing the credentials for PgBouncer to authenticate
	// against PostgreSQL server.
	ServerTLS *corev1.Secret

	// The TLS secret that will be used for client connections (application-side)
	ClientTLS *corev1.Secret

	// The root-CA that will be used to validate client certificates
	ClientCA *corev1.Secret

	// The CA that will be used to validate the connections to PostgreSQL
	ServerCA *corev1.Secret
}

// ConfigurationFiles is a set of configuration files that are needed for
// pgbouncer to work
type ConfigurationFiles map[string][]byte
