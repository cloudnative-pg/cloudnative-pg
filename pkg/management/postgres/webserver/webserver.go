/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package webserver contains the web server powering probes and backups
package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

var (
	// instance is the PostgreSQL instance to be collected
	instance *postgres.Instance

	// server is the HTTP server instance
	server *http.Server
)

func isServerHealthy(w http.ResponseWriter, r *http.Request) {
	err := instance.IsServerHealthy()
	if err != nil {
		log.Log.Info("Server doesn't look healthy", "err", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	_, _ = fmt.Fprint(w, "OK")
}

// This is the readiness probe
func isServerReady(w http.ResponseWriter, r *http.Request) {
	err := instance.IsServerReady()
	if err != nil {
		log.Log.Info("Server doesn't look ready", "err", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

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

	log.Log.V(2).Info("Cluster status extraction succeeded")

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

	backupCommand := postgres.NewBackupCommand(
		&cluster,
		&backup,
		typedClient,
		recorder,
		backupLog,
	)
	err = backupCommand.Start(ctx)
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
func Setup(serverInstance *postgres.Instance) {
	instance = serverInstance
}

// ListenAndServe starts a the web server handling probes
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
	serveMux.HandleFunc(url.PathReady, isServerReady)
	serveMux.HandleFunc(url.PathPgStatus, pgStatus)
	serveMux.HandleFunc(url.PathPgBackup,
		func(w http.ResponseWriter, r *http.Request) {
			requestBackup(typedClient, eventRecorder, w, r)
		},
	)

	server = &http.Server{Addr: fmt.Sprintf(":%d", url.StatusPort), Handler: serveMux}
	err = server.ListenAndServe()

	// The server has been shut down
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}

// Shutdown stops the web server
func Shutdown() error {
	if server == nil {
		return fmt.Errorf("server not started")
	}
	instance.ShutdownConnections()
	return server.Shutdown(context.Background())
}
