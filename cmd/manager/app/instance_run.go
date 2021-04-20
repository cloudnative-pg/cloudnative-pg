/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package app

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metricsserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
)

var (
	postgresCommand *exec.Cmd
	reconciler      *controller.InstanceReconciler
)

func runSubCommand(ctx context.Context) {
	var err error

	reconciler, err = controller.NewInstanceReconciler(&instance)
	if err != nil {
		log.Log.Error(err, "Error while starting reconciler")
		os.Exit(1)
	}

	_, err = instance.RefreshConfigurationFiles(ctx, reconciler.GetClient())
	if err != nil {
		log.Log.Error(err, "Error while writing the bootstrap configuration")
		os.Exit(1)
	}

	_, err = reconciler.RefreshServerCertificateFiles(ctx)
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS server certificates")
		os.Exit(1)
	}

	_, err = reconciler.RefreshReplicationUserCertificate(ctx)
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS server certificates")
		os.Exit(1)
	}

	_, err = reconciler.RefreshCA(ctx)
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS CA certificates")
		os.Exit(1)
	}

	err = reconciler.VerifyPgDataCoherence(ctx)
	if err != nil {
		log.Log.Error(err, "Error while checking Kubernetes cluster status")
		os.Exit(1)
	}

	if err = startWebServer(); err != nil {
		log.Log.Error(err, "Error while starting the web server")
		os.Exit(1)
	}

	startReconciler(ctx)
	registerSignalHandler()

	// Print the content of PostgreSQL control data, for debugging and tracing
	pgControlData := exec.Command("pg_controldata")
	pgControlData.Env = os.Environ()
	pgControlData.Env = append(pgControlData.Env, "PGDATA="+instance.PgData)
	pgControlData.Stdout = os.Stdout
	pgControlData.Stderr = os.Stderr
	err = pgControlData.Run()

	if err != nil {
		log.Log.Error(err, "Error printing the control information of this PostgreSQL instance")
		os.Exit(1)
	}

	postgresCommand, err = instance.Run()
	if err != nil {
		log.Log.Error(err, "Unable to start PostgreSQL up")
		os.Exit(1)
	}

	if err = postgresCommand.Wait(); err != nil {
		log.Log.Error(err, "PostgreSQL exited with errors")
		os.Exit(1)
	}
}

// startWebServer start the web server for handling probes given
// a certain PostgreSQL instance
func startWebServer() error {
	webserver.Setup(&instance)
	if err := metricsserver.Setup(&instance); err != nil {
		return err
	}

	go func() {
		err := webserver.ListenAndServe()
		if err != nil {
			log.Log.Error(err, "Error while starting the status web server")
		}
	}()

	go func() {
		err := metricsserver.ListenAndServe()
		if err != nil {
			log.Log.Error(err, "Error while starting the metrics server")
		}
	}()

	return nil
}

// startReconciler start the reconciliation loop
func startReconciler(ctx context.Context) {
	go reconciler.Run(ctx)
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func registerSignalHandler() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		log.Log.Info("Received termination signal", "signal", sig)

		log.Log.Info("Shutting down web server")
		err := webserver.Shutdown()
		if err != nil {
			log.Log.Error(err, "Error while shutting down the web server")
		} else {
			log.Log.Info("Web server shut down")
		}

		err = metricsserver.Shutdown()
		if err != nil {
			log.Log.Error(err, "Error while shutting down the metrics server")
		} else {
			log.Log.Info("Metrics server shut down")
		}

		log.Log.Info("Shutting down controller")
		reconciler.Stop()

		if postgresCommand != nil {
			log.Log.Info("Shutting down PostgreSQL instance")
			err := postgresCommand.Process.Signal(syscall.SIGINT)
			if err != nil {
				log.Log.Error(err, "Unable to send SIGINT to PostgreSQL instance")
			}
		}
	}()
}
