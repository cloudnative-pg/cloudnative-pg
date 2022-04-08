/*
Copyright 2019-2022 The CloudNativePG Contributors

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
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
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
func New(serverInstance *postgres.Instance) (*MetricsServer, error) {
	registry := prometheus.NewRegistry()
	exporter := NewExporter(serverInstance)
	if err := registry.Register(exporter); err != nil {
		return nil, fmt.Errorf("while registering PostgreSQL exporters: %w", err)
	}
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, fmt.Errorf("while registering Go exporters: %w", err)
	}
	serveMux := http.NewServeMux()
	serveMux.Handle(url.PathMetrics, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{Addr: fmt.Sprintf(":%d", url.PostgresMetricsPort), Handler: serveMux}

	metricServer := &MetricsServer{
		Webserver: webserver.NewWebServer(serverInstance, server),
		exporter:  exporter,
	}

	return metricServer, nil
}

// GetExporter get the exporter used for metrics. If the web statusServer still
// has not started, the exporter is nil
func (ms *MetricsServer) GetExporter() *Exporter {
	return ms.exporter
}
