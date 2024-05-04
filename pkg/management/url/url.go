/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package url holds the constants for webserver routing
package url

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
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

	// PathPGControlData is the URL path for PostgreSQL pg_controldata output
	PathPGControlData string = "/pg/controldata"

	// PathPgStatus is the URL path for PostgreSQL Status
	PathPgStatus string = "/pg/status"

	// PathPgBackup is the URL path for PostgreSQL Backup
	PathPgBackup string = "/pg/backup"

	// PathPgModeBackup is the URL path to interact with pg_start_backup and pg_stop_backup
	PathPgModeBackup string = "/pg/mode/backup"

	// PathMetrics is the URL path for Metrics
	PathMetrics string = "/metrics"

	// PathUpdate is the URL path for the instance manager update function
	PathUpdate string = "/update"

	// PathCache is the URL path for cached resources
	PathCache string = "/cache/"

	// StatusPort is the port for status HTTP requests
	StatusPort int = 8000
)

// Local builds an http request pointing to localhost
func Local(path string, port int) string {
	return build("http", "localhost", path, port)
}

// Build builds an url given the hostname and the path, pointing to the status web server
func Build(hostname, path string, port int) string {
	return build("https", hostname, path, port)
}

func build(scheme, hostname, path string, port int) string {
	// If path already starts with '/' we remove it
	if path[0] == '/' {
		path = path[1:]
	}
	return fmt.Sprintf("%s://%s:%d/%s", scheme, hostname, port, path)
}

// DoWithHTTPFallback perform a http.Request. In case of a HTTPS request returning ErrSchemeMismatch,
// it retries it using plain HTTP
func DoWithHTTPFallback(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if req.URL.Scheme == "https" && errors.Is(err, http.ErrSchemeMismatch) {
		ctx := req.Context()
		contextLog := log.FromContext(ctx)
		reqHTTP := req.Clone(ctx)
		reqHTTP.URL.Scheme = "http"
		contextLog.Warning("Downgrading HTTPS connection to HTTP", "URL", reqHTTP.URL)
		resp, err = client.Do(reqHTTP)
	}
	return resp, err
}
