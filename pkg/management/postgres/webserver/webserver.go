/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package webserver contains the web server included in PGK,
// the one powering probes
package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
)

var instance *postgres.Instance
var server *http.Server

// This is the readiness probe
func isServerHealthy(w http.ResponseWriter, r *http.Request) {
	err := instance.IsHealthy()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	_, _ = fmt.Fprint(w, "OK")
}

// This probe is for the instance status, including replication
func pgStatus(w http.ResponseWriter, r *http.Request) {
	status, err := instance.GetStatus()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	js, err := json.Marshal(status)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
}

// This function schedule a backup
func requestBackup(typedClient client.Client, w http.ResponseWriter, r *http.Request) {
	var cluster apiv1alpha1.Cluster
	var backup apiv1alpha1.Backup

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
		*cluster.Spec.Backup.BarmanObjectStore,
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

// ListenAndServe starts a the web server handling probes and metrics
func ListenAndServe(serverInstance *postgres.Instance) error {
	instance = serverInstance

	typedClient, err := management.NewClient()
	if err != nil {
		return fmt.Errorf("creating k8s client: %v", err)
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/healthz", isServerHealthy)
	serveMux.HandleFunc("/readyz", isServerHealthy)
	serveMux.HandleFunc("/pg/status", pgStatus)
	serveMux.HandleFunc("/pg/backup",
		func(w http.ResponseWriter, r *http.Request) {
			requestBackup(typedClient, w, r)
		},
	)

	// create the exporter and serve it on the /metrics endpoint
	registry := prometheus.NewRegistry()
	if err = registry.Register(metrics.NewExporter(instance)); err != nil {
		return fmt.Errorf("while registering PostgreSQL exporters: %w", err)
	}
	if err = registry.Register(prometheus.NewGoCollector()); err != nil {
		return fmt.Errorf("while registering Go exporters: %w", err)
	}
	serveMux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server = &http.Server{Addr: ":8000", Handler: serveMux}
	err = server.ListenAndServe()

	// The server has been shut down. Ok
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}

// Shutdown stops the web server
func Shutdown() error {
	if server == nil {
		return errors.New("server not started")
	}
	instance.ShutdownConnections()
	return server.Shutdown(context.Background())
}
