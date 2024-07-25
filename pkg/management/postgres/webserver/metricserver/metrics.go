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

package metricserver

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// MetricsServer exposes the metrics of the postgres instance
type MetricsServer struct {
	*webserver.Webserver

	// exporter is the exporter for predefined queries and for
	// custom ones
	exporter *Exporter
}

// New configure the web statusServer for a certain PostgreSQL instance, and
// must be invoked before starting the real web statusServer
func New(serverInstance *postgres.Instance, exporter *Exporter) (*MetricsServer, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(exporter); err != nil {
		return nil, fmt.Errorf("while registering PostgreSQL exporters: %w", err)
	}
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, fmt.Errorf("while registering Go exporters: %w", err)
	}
	serveMux := http.NewServeMux()
	serveMux.Handle(url.PathMetrics, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", url.PostgresMetricsPort),
		Handler:           serveMux,
		ReadTimeout:       webserver.DefaultReadTimeout,
		ReadHeaderTimeout: webserver.DefaultReadHeaderTimeout,
	}

	if serverInstance.MetricsPortTLS {
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
			GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return serverInstance.ServerCertificate, nil
			},
		}
	}

	metricServer := &MetricsServer{
		Webserver: webserver.NewWebServer(server),
		exporter:  exporter,
	}

	return metricServer, nil
}
