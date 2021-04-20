/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package metricsserver contains the web server powering metrics
package metricsserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

var (
	// instance is the PostgreSQL instance to be collected
	instance *postgres.Instance

	// metricsServer is the HTTP metrics server instance
	server *http.Server

	// registry is the Prometheus query registry
	registry *prometheus.Registry

	// exporter is the exporter for predefined queries and for
	// custom ones
	exporter *metrics.Exporter
)

// Setup configure the web statusServer for a certain PostgreSQL instance, and
// must be invoked before starting the real web statusServer
func Setup(serverInstance *postgres.Instance) error {
	instance = serverInstance

	// create the exporter and serve it on the /metrics endpoint
	registry = prometheus.NewRegistry()
	exporter = metrics.NewExporter(instance)
	if err := registry.Register(exporter); err != nil {
		return fmt.Errorf("while registering PostgreSQL exporters: %w", err)
	}
	if err := registry.Register(prometheus.NewGoCollector()); err != nil {
		return fmt.Errorf("while registering Go exporters: %w", err)
	}

	return nil
}

// ListenAndServe starts a the web server handling metrics
func ListenAndServe() error {
	if instance == nil {
		return fmt.Errorf("metrics web server is still not set up")
	}

	serveMux := http.NewServeMux()
	serveMux.Handle(url.PathMetrics, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server = &http.Server{Addr: fmt.Sprintf(":%d", url.MetricsPort), Handler: serveMux}
	err := server.ListenAndServe()

	// The metricsServer has been shut down
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}

// GetExporter get the exporter used for metrics. If the web statusServer still
// has not started, the exporter is nil
func GetExporter() *metrics.Exporter {
	return exporter
}

// Shutdown stops the web metrics server
func Shutdown() error {
	if server == nil {
		return fmt.Errorf("metricsserver not started")
	}
	instance.ShutdownConnections()
	return server.Shutdown(context.Background())
}
