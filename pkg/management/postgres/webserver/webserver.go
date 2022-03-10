/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package webserver

import (
	"context"
	"net/http"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// Webserver contains a server that interacts with postgres instance
type Webserver struct {
	// instance is the PostgreSQL instance to be collected
	instance *postgres.Instance
	server   *http.Server
}

// NewWebServer creates a Webserver given a postgres.Instance and a http.Server
func NewWebServer(instance *postgres.Instance, server *http.Server) *Webserver {
	return &Webserver{
		instance: instance,
		server:   server,
	}
}

// Start implements the runnable interface
func (ws *Webserver) Start(ctx context.Context) error {
	errChan := make(chan error, 1)
	go func() {
		log.Info("Starting webserver", "address", ws.server.Addr)

		err := ws.server.ListenAndServe()
		if err != nil {
			errChan <- err
		}
	}()

	select {
	// we exit with error code, potentially we could do a retry logic, but rarely a webserver that doesn't start will run
	// on subsequent tries
	case err := <-errChan:
		log.Error(err, "Error while starting the web server", "address", ws.server.Addr)
		return err
	case <-ctx.Done():
		if err := ws.server.Shutdown(context.Background()); err != nil {
			log.Error(err, "Error while shutting down the web server", "address", ws.server.Addr)
			return err
		}
	}

	log.Info("Webserver exited", "address", ws.server.Addr)

	return nil
}
