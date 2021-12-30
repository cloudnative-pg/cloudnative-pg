/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package run implements the "instance run" subcommand of the operator
package run

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metricsserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// NewCmd creates the "instance run" subcommand
func NewCmd() *cobra.Command {
	var pgData string
	var podName string
	var clusterName string
	var namespace string

	cmd := &cobra.Command{
		Use: "run [flags]",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			instance := postgres.NewInstance()

			instance.PgData = pgData
			instance.Namespace = namespace
			instance.PodName = podName
			instance.ClusterName = clusterName

			return runSubCommand(ctx, instance)
		},
	}

	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be started up")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")

	return cmd
}

func runSubCommand(ctx context.Context, instance *postgres.Instance) error {
	var err error

	log.Info("Starting Cloud Native PostgreSQL Instance Manager",
		"version", versions.Version,
		"build", versions.Info)

	reconciler, err := controller.NewInstanceReconciler(instance)
	if err != nil {
		log.Error(err, "Error while creating reconciler")
		return err
	}
	var cluster apiv1.Cluster
	err = reconciler.GetClient().Get(ctx,
		ctrl.ObjectKey{Namespace: instance.Namespace, Name: instance.ClusterName},
		&cluster)
	if err != nil {
		log.Error(err, "Error while getting cluster")
		return err
	}

	err = reconciler.UpdateCacheFromCluster(ctx, &cluster)
	if err != nil {
		log.Error(err, "Error while initializing cache")
		return err
	}

	_, err = instance.RefreshConfigurationFilesFromCluster(&cluster)
	if err != nil {
		log.Error(err, "Error while writing the bootstrap configuration")
		return err
	}

	if err = logpipe.Start(); err != nil {
		log.Error(err, "Error while starting the logging collector routine")
		return err
	}

	if err = startWebServer(instance); err != nil {
		log.Error(err, "Error while starting the web server")
		return err
	}

	_, err = reconciler.RefreshServerCertificateFiles(ctx, &cluster)
	if err != nil {
		log.Error(err, "Error while writing the TLS server certificates")
		return err
	}

	_, err = reconciler.RefreshReplicationUserCertificate(ctx, &cluster)
	if err != nil {
		log.Error(err, "Error while writing the TLS server certificates")
		return err
	}

	_, err = reconciler.RefreshClientCA(ctx, &cluster)
	if err != nil {
		log.Error(err, "Error while writing the TLS CA Client certificates")
		return err
	}

	_, err = reconciler.RefreshServerCA(ctx, &cluster)
	if err != nil {
		log.Error(err, "Error while writing the TLS CA Server certificates")
		return err
	}

	err = reconciler.VerifyPgDataCoherence(ctx, &cluster)
	if err != nil {
		log.Error(err, "Error while checking Kubernetes cluster status")
		return err
	}

	primary, err := instance.IsPrimary()
	if err != nil {
		log.Error(err, "Error while getting the primary status")
		return err
	}

	if !primary {
		err = reconciler.RefreshReplicaConfiguration(ctx)
		if err != nil {
			log.Error(err, "Error while creating the replica configuration")
			return err
		}
	}

	startReconciler(ctx, reconciler)

	instance.LogPgControldata()

	streamingCmd, err := instance.Run()
	if err != nil {
		log.Error(err, "Unable to start PostgreSQL up")
		return err
	}

	registerSignalHandler(reconciler, cluster.GetMaxStopDelay())

	if err = streamingCmd.Wait(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			log.Error(err, "Error waiting on PostgreSQL process")
		} else {
			log.Error(exitError, "PostgreSQL process exited with errors")
		}
	}

	instance.LogPgControldata()

	return err
}

// startWebServer start the web server for handling probes given
// a certain PostgreSQL instance
func startWebServer(instance *postgres.Instance) error {
	webserver.Setup(instance)
	if err := metricsserver.Setup(instance); err != nil {
		return err
	}

	go func() {
		err := webserver.ListenAndServe()
		if err != nil {
			log.Error(err, "Error while starting the status web server")
		}
	}()

	go func() {
		err := webserver.LocalListenAndServe()
		if err != nil {
			log.Error(err, "Error while starting the local server")
		}
	}()

	go func() {
		err := metricsserver.ListenAndServe()
		if err != nil {
			log.Error(err, "Error while starting the metrics server")
		}
	}()

	return nil
}

// startReconciler start the reconciliation loop
func startReconciler(ctx context.Context, reconciler *controller.InstanceReconciler) {
	go reconciler.Run(ctx)
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func registerSignalHandler(reconciler *controller.InstanceReconciler, maxStopDelay int32) {
	// We need to shut down the postmaster in a certain time (dictated by `cluster.GetMaxStopDelay()`).
	// For half of this time, we are waiting for connections to go down, the other half
	// we just handle the shutdown procedure itself.
	smartShutdownTimeout := int(maxStopDelay) / 2

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error

		sig := <-signals
		log.Info("Received termination signal", "signal", sig)

		log.Info("Shutting down the metrics server")
		err = metricsserver.Shutdown()
		if err != nil {
			log.Error(err, "Error while shutting down the metrics server")
		} else {
			log.Info("Metrics server shut down")
		}

		log.Info("Shutting down controller")
		reconciler.Stop()

		log.Info("Requesting smart shutdown of the PostgreSQL instance")
		err = reconciler.Instance().Shutdown(postgres.ShutdownOptions{
			Mode:    postgres.ShutdownModeSmart,
			Wait:    true,
			Timeout: &smartShutdownTimeout,
		})
		if err != nil {
			log.Warning("Error while handling the smart shutdown request: requiring fast shutdown",
				"err", err)
			err = reconciler.Instance().Shutdown(postgres.ShutdownOptions{
				Mode: postgres.ShutdownModeFast,
				Wait: true,
			})
		}
		if err != nil {
			log.Error(err, "Error while shutting down the PostgreSQL instance")
		} else {
			log.Info("PostgreSQL instance shut down")
		}

		// We can't shut down the web server before shutting down PostgreSQL.
		// PostgreSQL need it because the wal-archive process need to be able
		// to his job doing the PostgreSQL shut down.
		log.Info("Shutting down web server")
		err = webserver.Shutdown()
		if err != nil {
			log.Error(err, "Error while shutting down the web server")
		} else {
			log.Info("Web server shut down")
		}
	}()
}
