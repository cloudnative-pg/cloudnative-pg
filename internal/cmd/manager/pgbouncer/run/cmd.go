/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package run implements the "pgbouncer run" subcommand of the operator
package run

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/internal/pgbouncer/management/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/metricsserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// NewCmd creates the "instance run" subcommand
func NewCmd() *cobra.Command {
	var (
		poolerNamespacedName types.NamespacedName
		metricsTLS           bool

		errorMissingPoolerNamespacedName = fmt.Errorf("missing pooler name or namespace")
	)

	const (
		poolerNameEnvVar      = "POOLER_NAME"
		poolerNamespaceEnvVar = "NAMESPACE"
		metricsPortTLSEnvVar  = "METRICS_PORT_TLS"
	)

	cmd := &cobra.Command{
		Use:           "run",
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			contextLogger := log.FromContext(cmd.Context())
			if poolerNamespacedName.Name == "" || poolerNamespacedName.Namespace == "" {
				contextLogger.Info(
					"pooler object key not set",
					"poolerNamespacedName", poolerNamespacedName)
				return errorMissingPoolerNamespacedName
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := log.IntoContext(
				cmd.Context(),
				log.GetLogger().WithValues("logger", "pgbouncer-manager"),
			)
			contextLogger := log.FromContext(ctx)

			opts := runOptions{
				poolerNamespacedName: poolerNamespacedName,
				metricsPortTLS:       metricsTLS,
			}
			if err := runSubCommand(ctx, opts); err != nil {
				contextLogger.Error(err, "Error while running manager")
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(
		&poolerNamespacedName.Name,
		"pooler-name",
		os.Getenv(poolerNameEnvVar),
		"The name of the Pooler in k8s, used to generate configuration and refresh pgbouncer when needed. "+
			"Defaults to the value of the POOLER_NAME environment variable")
	cmd.Flags().StringVar(
		&poolerNamespacedName.Namespace,
		"namespace",
		os.Getenv(poolerNamespaceEnvVar),
		"The namespace of the cluster and of the Pod in k8s. "+
			"Defaults to the value of the NAMESPACE environment variable")
	cmd.Flags().BoolVar(
		&metricsTLS,
		"metrics-port-tls",
		boolFromEnv(metricsPortTLSEnvVar),
		"Enable TLS for the metrics endpoint. "+
			"Defaults to the value of the METRICS_PORT_TLS environment variable",
	)
	return cmd
}

func runSubCommand(ctx context.Context, opts runOptions) error {
	var err error

	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Starting CloudNativePG PgBouncer Instance Manager",
		"version", versions.Version,
		"build", versions.Info,
		"metricsPortTLS", opts.metricsPortTLS)

	if err = startWebServer(ctx, opts.metricsTLSConfig()); err != nil {
		return fmt.Errorf("while starting the web server: %w", err)
	}

	reconciler, err := controller.NewPgBouncerReconciler(opts.poolerNamespacedName)
	if err != nil {
		return fmt.Errorf("while initializing the new reconciler: %w", err)
	}

	err = reconciler.Init(ctx)
	if err != nil {
		return fmt.Errorf("while initializing reconciler: %w", err)
	}

	// Start PgBouncer with the generated configuration
	const pgBouncerCommandName = "/usr/bin/pgbouncer"
	pgBouncerIni := filepath.Join(config.ConfigsDir, config.PgBouncerIniFileName)
	pgBouncerCmd := exec.Command(pgBouncerCommandName, pgBouncerIni) //nolint:gosec
	stdoutWriter := &execlog.LogWriter{
		Logger: contextLogger.WithValues(execlog.PipeKey, execlog.StdOut),
	}
	stderrWriter := &pgBouncerLogWriter{
		Logger: contextLogger.WithValues(execlog.PipeKey, execlog.StdErr),
	}
	streamingCmd, err := execlog.RunStreamingNoWaitWithWriter(
		pgBouncerCmd, pgBouncerCommandName, stdoutWriter, stderrWriter)
	if err != nil {
		return fmt.Errorf("running pgbouncer: %w", err)
	}

	startReconciler(ctx, reconciler)
	registerSignalHandler(ctx, reconciler, pgBouncerCmd)

	if err = streamingCmd.Wait(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			contextLogger.Error(err, "Error waiting on pgbouncer process")
		} else {
			contextLogger.Error(exitError, "pgbouncer process exited with errors")
		}
		return err
	}

	return nil
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func registerSignalHandler(ctx context.Context, reconciler *controller.PgBouncerReconciler, command *exec.Cmd) {
	contextLogger := log.FromContext(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		contextLogger.Info("Received termination signal", "signal", sig)

		contextLogger.Info("Shutting down web server")
		err := metricsserver.Shutdown()
		if err != nil {
			contextLogger.Error(err, "Error while shutting down the metrics server")
		} else {
			contextLogger.Info("Metrics server shut down")
		}

		reconciler.Stop()

		if command != nil {
			contextLogger.Info("Shutting down pgbouncer instance")
			err := command.Process.Signal(syscall.SIGINT)
			if err != nil {
				contextLogger.Error(err, "Unable to send SIGINT to pgbouncer instance")
			}
		}
	}()
}

// startWebServer starts the web server for exposing metrics given
// a certain PgBouncer instance
func startWebServer(ctx context.Context, tlsConfig *tls.Config) error {
	contextLogger := log.FromContext(ctx)
	if err := metricsserver.Setup(ctx); err != nil {
		return err
	}

	go func() {
		err := metricsserver.ListenAndServe(tlsConfig)
		if err != nil {
			contextLogger.Error(err, "Error while starting the metrics server")
		}
	}()

	return nil
}

// startReconciler start the reconciliation loop
func startReconciler(ctx context.Context, reconciler *controller.PgBouncerReconciler) {
	go reconciler.Run(ctx)
}

// boolFromEnv reads a boolean value from the given environment variable.
// Returns false when the variable is unset. Terminates the process with a
// fatal error written to stderr when the variable is set to a value that
// strconv.ParseBool cannot parse, to avoid silently ignoring an operator
// misconfiguration. stderr is used directly because os.Exit skips deferred
// log-sink flushes and a structured log entry could otherwise be buffered
// and lost.
func boolFromEnv(envVar string) bool {
	str := os.Getenv(envVar)
	if str == "" {
		return false
	}
	val, err := strconv.ParseBool(str)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"invalid boolean value %q for environment variable %s: %v\n",
			str, envVar, err)
		os.Exit(1)
	}
	return val
}
