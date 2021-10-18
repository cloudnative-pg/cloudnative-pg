/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package run implements the "pgbouncer run" subcommand of the operator
package run

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/pgbouncer/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/pgbouncer/metricsserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// NewCmd creates the "instance run" subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubCommand(cmd.Context())
		},
	}

	return cmd
}

func runSubCommand(ctx context.Context) error {
	var err error

	log.Info("Starting Cloud Native PostgreSQL PgBouncer Instance Manager",
		"version", versions.Version,
		"build", versions.Info)

	if err = startWebServer(); err != nil {
		log.Error(err, "Error while starting the web server")
		return err
	}

	reconciler, err := controller.NewPgBouncerReconciler(os.Getenv("POOLER_NAME"), os.Getenv("NAMESPACE"))
	if err != nil {
		log.Error(err, "Error while initializing new Reconciler")
		return err
	}

	err = fileutils.EnsureDirectoryExist(pgbouncer.PgBouncerSocketDir)
	if err != nil {
		log.Error(err, "while checking socket directory existed", "dir", pgbouncer.PgBouncerSocketDir)
	}
	// Print the content of PostgreSQL control data, for debugging and tracing
	const pgBouncerCommandName = "/usr/bin/pgbouncer"
	pgBouncerCmd := exec.Command(pgBouncerCommandName, "/config/pgbouncer.ini")
	pgBouncerCmd.Env = os.Environ()
	stdoutWriter := &execlog.LogWriter{
		Logger: log.WithValues(execlog.PipeKey, execlog.StdOut),
	}
	stderrWriter := &pgBouncerLogWriter{
		Logger: log.WithValues(execlog.PipeKey, execlog.StdErr),
	}
	err = execlog.RunStreamingNoWaitWithWriter(pgBouncerCmd, pgBouncerCommandName, stdoutWriter, stderrWriter)
	if err != nil {
		log.Error(err, "Error running Pg Bouncer")
		return err
	}

	startReconciler(ctx, reconciler)

	registerSignalHandler(reconciler, pgBouncerCmd)

	if err = pgBouncerCmd.Wait(); err != nil {
		log.Error(err, "pgbouncer exited with errors")
		return err
	}

	return nil
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func registerSignalHandler(reconciler *controller.PgBouncerReconciler, command *exec.Cmd) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		log.Info("Received termination signal", "signal", sig)

		log.Info("Shutting down web server")
		err := metricsserver.Shutdown()
		if err != nil {
			log.Error(err, "Error while shutting down the metrics server")
		} else {
			log.Info("Metrics server shut down")
		}

		reconciler.Stop()

		if command != nil {
			log.Info("Shutting down pgbouncer instance")
			err := command.Process.Signal(syscall.SIGINT)
			if err != nil {
				log.Error(err, "Unable to send SIGINT to pgbouncer instance")
			}
		}
	}()
}

// startWebServer start the web server for handling probes given
// a certain PostgreSQL instance
func startWebServer() error {
	if err := metricsserver.Setup(); err != nil {
		return err
	}

	go func() {
		err := metricsserver.ListenAndServe()
		if err != nil {
			log.Error(err, "Error while starting the metrics server")
		}
	}()

	return nil
}

// startReconciler start the reconciliation loop
func startReconciler(ctx context.Context, reconciler *controller.PgBouncerReconciler) {
	go reconciler.Run(ctx)
}
