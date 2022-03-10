/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/upgrade"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

type remoteWebserverEndpoints struct {
	typedClient client.Client
	instance    *postgres.Instance
}

// NewRemoteWebServer returns a webserver that allows connection from external clients
func NewRemoteWebServer(instance *postgres.Instance) (*Webserver, error) {
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, fmt.Errorf("creating controller-runtine client: %v", err)
	}

	endpoints := remoteWebserverEndpoints{
		typedClient: typedClient,
		instance:    instance,
	}
	serveMux := http.NewServeMux()
	serveMux.HandleFunc(url.PathHealth, endpoints.isServerHealthy)
	serveMux.HandleFunc(url.PathReady, endpoints.isServerReady)
	serveMux.HandleFunc(url.PathPgStatus, endpoints.pgStatus)
	serveMux.HandleFunc(url.PathUpdate, endpoints.updateInstanceManager)

	server := &http.Server{Addr: fmt.Sprintf(":%d", url.StatusPort), Handler: serveMux}

	return NewWebServer(instance, server), nil
}

func (ws *remoteWebserverEndpoints) isServerHealthy(w http.ResponseWriter, r *http.Request) {
	// If `pg_rewind` is running the Pod is starting up.
	// We need to report it healthy to avoid being killed by the kubelet.
	// Same goes for instances with fencing on.
	if !ws.instance.PgRewindIsRunning && !ws.instance.FencingOn.Load() {
		err := ws.instance.IsServerHealthy()
		if err != nil {
			log.Info("Liveness probe failing", "err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	log.Trace("Liveness probe succeeding")

	_, _ = fmt.Fprint(w, "OK")
}

// This is the readiness probe
func (ws *remoteWebserverEndpoints) isServerReady(w http.ResponseWriter, r *http.Request) {
	err := ws.instance.IsServerReady()
	if err != nil {
		log.Info("Readiness probe failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Trace("Readiness probe succeeding")

	_, _ = fmt.Fprint(w, "OK")
}

// This probe is for the instance status, including replication
func (ws *remoteWebserverEndpoints) pgStatus(w http.ResponseWriter, r *http.Request) {
	// Extract the status of the current instance
	status, err := ws.instance.GetStatus()
	switch {
	case err != nil && !ws.instance.FencingOn.Load():
		log.Info(
			"Instance status probe failing",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return

	case err != nil && ws.instance.FencingOn.Load():
		log.Info("fencing enabled, will fake status")
		// force reporting fencing as enabled
		status.IsFencingOn = true
		// force reporting the instance as primary if required
		status.IsPrimary, err = ws.instance.IsPrimary()
		if err != nil {
			log.Info(
				"Internal error checking if primary",
				"err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// force the instance to be reported as ready
		status.IsReady = true
	}

	// Marshal the status back to the operator
	log.Trace("Instance status probe succeeding")
	js, err := json.Marshal(status)
	if err != nil {
		log.Info(
			"Internal error marshalling instance status",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
}

// updateInstanceManager replace the instance with one in the
// new binary
func (ws *remoteWebserverEndpoints) updateInstanceManager(w http.ResponseWriter, r *http.Request) {
	// No need to handle this request if it is not a put
	if r.Method != http.MethodPut {
		http.Error(w, "wrong method used", http.StatusMethodNotAllowed)
		return
	}

	// No need to do anything if we are already upgrading
	if ws.instance.InstanceManagerIsUpgrading {
		http.Error(w, "instance manager is already upgrading", http.StatusTeapot)
		return
	}

	err := upgrade.FromReader(ws.typedClient, ws.instance, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Unfortunately this point, if everything is right, will not be reached.
	// At this stage we are running the new version of the instance manager
	// and not the old one.
	_, _ = fmt.Fprint(w, "OK")
}
