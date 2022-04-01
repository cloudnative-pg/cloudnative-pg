/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package url holds the constants for webserver routing
package url

import (
	"fmt"
)

const (
	// LocalPort is the port for only available from Postgres.
	LocalPort int = 8010

	// PostgresMetricsPort is the port for the exporter of PostgreSQL related metrics (HTTP)
	PostgresMetricsPort int = 9187

	// PgBouncerMetricsPort is the port for the exporter of PgBouncer related metrics (HTTP)
	PgBouncerMetricsPort int = 9127

	// PathHealth is the URL path for Health State
	PathHealth string = "/healthz"

	// PathReady is the URL oath for Ready State
	PathReady string = "/readyz"

	// PathPgStatus is the URL path for PostgreSQL Status
	PathPgStatus string = "/pg/status"

	// PathPgBackup is the URL path for PostgreSQL Backup
	PathPgBackup string = "/pg/backup"

	// PathMetrics is the URL path for Metrics
	PathMetrics string = "/metrics"

	// PathUpdate is the URL path for the instance manager update function
	PathUpdate string = "/update"

	// PathCache is the URL path for cached resources
	PathCache string = "/cache/"

	// StatusPort is the port for status HTTP requests
	StatusPort int = 8000
)

// Local builds an url for the provided path on localhost, pointing to the status web server
func Local(path string, port int) string {
	return Build("localhost", path, port)
}

// Build builds an url given the hostname and the path, pointing to the status web server
func Build(hostname, path string, port int) string {
	// If path already starts with '/' we remove it
	if path[0] == '/' {
		path = path[1:]
	}
	return fmt.Sprintf("http://%s:%d/%s", hostname, port, path)
}
