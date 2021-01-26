/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// WalRestoreCommand restore a certain WAL file from the cloud
// using barman-wal-restore
func WalRestoreCommand(args []string) {
	var clusterName string
	var namespace string
	var podName string

	flag.StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s")
	flag.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the "+
		"current pod in k8s")
	flag.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and the Pods in k8s")

	_ = flag.CommandLine.Parse(args)
	if len(flag.Args()) != 2 {
		fmt.Println("Usage: manager wal-restore <wal_name> <destination_path>")
		os.Exit(1)
	}
	walName := flag.Arg(0)
	destinationPath := flag.Arg(1)

	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		log.Log.Error(err, "Error while creating k8s client")
		os.Exit(1)
	}

	var cluster apiv1alpha1.Cluster
	err = typedClient.Get(context.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)
	if err != nil {
		log.Log.Error(err, "Error while getting the cluster status")
		os.Exit(1)
	}

	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		// Backup not configured, skipping WAL
		log.Log.V(4).Info("Skipping WAL restore, there is no backup configuration",
			"walName", walName,
			"pod", podName,
			"cluster", clusterName,
			"namespace", namespace,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		os.Exit(1)
	}

	if cluster.Status.CurrentPrimary == podName {
		// Why a request to restore a WAL file is arriving from the master server?
		// Something strange is happening here
		log.Log.Info("Received request to restore a WAL file on the current primary",
			"walName", walName,
			"pod", podName,
			"cluster", clusterName,
			"namespace", namespace,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		os.Exit(1)
	}

	configuration := cluster.Spec.Backup.BarmanObjectStore

	var options []string
	if configuration.Wal != nil {
		if len(configuration.Wal.Encryption) != 0 {
			options = append(
				options,
				"-e",
				string(configuration.Wal.Encryption))
		}
	}

	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}

	serverName := clusterName
	if len(configuration.ServerName) != 0 {
		serverName = configuration.ServerName
	}

	options = append(
		options,
		configuration.DestinationPath,
		serverName,
		walName,
		destinationPath)

	cmd := exec.Command("barman-cloud-wal-restore", options...) // #nosec G204
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Log.Info("barman-cloud-wal-restore",
			"walName", walName,
			"pod", podName,
			"cluster", clusterName,
			"namespace", namespace,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
			"options", options,
			"exitCode", cmd.ProcessState.ExitCode(),
		)
		os.Exit(1)
	}
}
