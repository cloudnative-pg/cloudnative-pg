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

// Package url holds the constants for webserver routing
package url

import (
	"fmt"
)

const (
	// LocalPort is the port for only available from Postgres.
	LocalPort int32 = 8010

	// PostgresMetricsPort is the port for the exporter of PostgreSQL related metrics (HTTP)
	PostgresMetricsPort int32 = 9187

	// PgBouncerMetricsPort is the port for the exporter of PgBouncer related metrics (HTTP)
	PgBouncerMetricsPort int32 = 9127

	// PathFailSafe is the path for the failsafe entrypoint
	PathFailSafe string = "/failsafe"

	// PathHealth is the URL path for Health State
	PathHealth string = "/healthz"

	// PathReady is the URL path for Ready State
	PathReady string = "/readyz"

	// PathStartup is the URL path for the Startup probe
	PathStartup string = "/startupz"

	// PathPGControlData is the URL path for PostgreSQL pg_controldata output
	PathPGControlData string = "/pg/controldata"

	// PathPgStatus is the URL path for PostgreSQL Status
	PathPgStatus string = "/pg/status"

	// PathWALArchiveStatusCondition is the URL path for setting the wal-archive condition on the Cluster object
	PathWALArchiveStatusCondition string = "/cluster/status/condition/wal/archive"

	// PathPgBackup is the URL path for PostgreSQL Backup
	PathPgBackup string = "/pg/backup"

	// PathPgModeBackup is the URL path to interact with pg_start_backup and pg_stop_backup
	PathPgModeBackup string = "/pg/mode/backup"

	// PathPgArchivePartial is the URL path to interact with the partial wal archive
	PathPgArchivePartial string = "/pg/archive/partial"

	// PathMetrics is the URL path for Metrics
	PathMetrics string = "/metrics"

	// PathUpdate is the URL path for the instance manager update function
	PathUpdate string = "/update"

	// PathCache is the URL path for cached resources
	PathCache string = "/cache/"

	// StatusPort is the port for status HTTP requests
	StatusPort int32 = 8000
)

// Local builds an http request pointing to localhost
func Local(path string, port int32) string {
	return Build("http", "localhost", path, port)
}

// Build builds an url given the hostname and the path, pointing to the status web server
func Build(scheme, hostname, path string, port int32) string {
	// If path already starts with '/' we remove it
	if path[0] == '/' {
		path = path[1:]
	}
	return fmt.Sprintf("%s://%s:%d/%s", scheme, hostname, port, path)
}
