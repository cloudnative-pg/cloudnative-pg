/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package webserver contains the web server included in PGK,
// the one powering probes
package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

var (
	// instance is the PostgreSQL instance to be collected
	instance *postgres.Instance

	// server is the HTTP server instance
	server *http.Server

	// registry is the Prometheus query registry
	registry *prometheus.Registry

	// exporter is the exporter for predefined queries and for
	// custom ones
	exporter *metrics.Exporter
)

// This is the readiness probe
func isServerHealthy(w http.ResponseWriter, r *http.Request) {
	err := instance.IsHealthy()
	if err != nil {
		log.Log.Info("Server doesn't look healthy", "err", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	log.Log.V(1).Info("Readiness probe succeeded")

	_, _ = fmt.Fprint(w, "OK")
}

// This probe is for the instance status, including replication
func pgStatus(w http.ResponseWriter, r *http.Request) {
	status, err := instance.GetStatus()
	if err != nil {
		log.Log.Info(
			"Server doesn't look healthy, cannot extract instance status",
			"err", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	log.Log.V(1).Info("Cluster status extraction succeeded")

	js, err := json.Marshal(status)
	if err != nil {
		log.Log.Info(
			"Internal error marshalling instance status",
			"err", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
}

// This function schedule a backup
func requestBackup(typedClient client.Client, recorder record.EventRecorder, w http.ResponseWriter, r *http.Request) {
	var cluster apiv1.Cluster
	var backup apiv1.Backup

	ctx := context.Background()

	backupName := r.URL.Query().Get("name")
	if len(backupName) == 0 {
		http.Error(w, "Missing backup name parameter", 400)
		return
	}

	err := typedClient.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      instance.ClusterName,
	}, &cluster)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("error while getting cluster: %v", err.Error()),
			500)
		return
	}

	err = typedClient.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      backupName,
	}, &backup)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("error while getting backup: %v", err.Error()),
			500)
		return
	}

	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		http.Error(w, "Backup not configured in the cluster", http.StatusConflict)
		return
	}

	backupLog := log.Log.WithValues(
		"backupName", backup.Name,
		"backupNamespace", backup.Name)

	err = instance.Backup(
		ctx,
		typedClient,
		recorder,
		&cluster,
		&backup,
		backupLog)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("error while starting backup: %v", err.Error()),
			500)
		return
	}

	_, _ = fmt.Fprint(w, "OK")
}

// Setup configure the web server for a certain PostgreSQL instance, and
// must be invoked before starting the real web server
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

// ListenAndServe starts a the web server handling probes and metrics
func ListenAndServe() error {
	if instance == nil {
		return fmt.Errorf("web server is still not set up")
	}

	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return fmt.Errorf("creating controller-runtine client: %v", err)
	}

	eventRecorder, err := management.NewEventRecorder()
	if err != nil {
		return fmt.Errorf("creating kubernetes event recorder: %v", err)
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc(url.PathHealth, isServerHealthy)
	serveMux.HandleFunc(url.PathReady, isServerHealthy)
	serveMux.HandleFunc(url.PathPgStatus, pgStatus)
	serveMux.HandleFunc(url.PathPgBackup,
		func(w http.ResponseWriter, r *http.Request) {
			requestBackup(typedClient, eventRecorder, w, r)
		},
	)
	serveMux.Handle(url.PathMetrics, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server = &http.Server{Addr: fmt.Sprintf(":%d", url.Port), Handler: serveMux}
	err = server.ListenAndServe()

	// The server has been shut down. Ok
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}

// GetExporter get the exporter used for metrics. If the web server still
// has not started, the exporter is nil
func GetExporter() *metrics.Exporter {
	return exporter
}

// Shutdown stops the web server
func Shutdown() error {
	if server == nil {
		return fmt.Errorf("server not started")
	}
	instance.ShutdownConnections()
	return server.Shutdown(context.Background())
}
