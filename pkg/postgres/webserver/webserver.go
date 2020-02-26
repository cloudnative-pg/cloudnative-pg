/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/postgres"
)

var instance *postgres.Instance
var server *http.Server

func isServerHealthy(w http.ResponseWriter, r *http.Request) {
	err := instance.IsHealthy()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	_, _ = fmt.Fprint(w, "OK")
}

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

// ListenAndServe starts a the web server handling probes
func ListenAndServe(serverInstance *postgres.Instance) error {
	instance = serverInstance

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/healthz", isServerHealthy)
	serveMux.HandleFunc("/readyz", isServerHealthy)
	serveMux.HandleFunc("/pg/status", pgStatus)

	server = &http.Server{Addr: ":8000", Handler: serveMux}
	err := server.ListenAndServe()

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
