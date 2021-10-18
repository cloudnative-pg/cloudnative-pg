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
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

var (
	// metricsServer is the HTTP metrics server instance
	server *http.Server

	// registry is the Prometheus query registry
	registry *prometheus.Registry

	// exporter is the exporter for predefined queries and for
	// custom ones
	exporter *Exporter
)

// Setup configure the web statusServer for a certain PostgreSQL instance, and
// must be invoked before starting the real web statusServer
func Setup() error {
	// create the exporter and serve it on the /metrics endpoint
	registry = prometheus.NewRegistry()
	exporter = NewExporter()
	if err := registry.Register(exporter); err != nil {
		return fmt.Errorf("while registering PgBouncer exporters: %w", err)
	}
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return fmt.Errorf("while registering Go exporters: %w", err)
	}
	return nil
}

// ListenAndServe starts a the web server handling metrics
func ListenAndServe() error {
	serveMux := http.NewServeMux()
	serveMux.Handle(url.PathMetrics, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server = &http.Server{Addr: fmt.Sprintf(":%d", url.PgBouncerMetricsPort), Handler: serveMux}
	err := server.ListenAndServe()

	// The metricsServer has been shut down
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}

// Shutdown stops the web metrics server
func Shutdown() error {
	return server.Shutdown(context.Background())
}
