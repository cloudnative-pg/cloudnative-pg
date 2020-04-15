/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
)

// WalArchiveCommand archives a certain WAL into the cloud
// using barman-wal-archive
func WalArchiveCommand(args []string) {
	var clusterName string
	var namespace string
	var podName string

	flag.StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s")
	flag.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the "+
		"current pod in k8s")
	flag.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")

	_ = flag.CommandLine.Parse(args)
	if len(flag.Args()) != 1 {
		fmt.Println("Usage: manager wal-archive <file>")
		os.Exit(1)
	}
	walName := flag.Arg(0)

	typedClient, err := management.NewClient()
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

	if cluster.Spec.Backup == nil || len(cluster.Spec.Backup.DestinationPath) == 0 {
		// Backup not configured, skipping WAL
		log.Log.Info("Skipping WAL",
			"walName", walName,
			"pod", podName,
			"cluster", clusterName,
			"namespace", namespace,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		return
	}

	if cluster.Status.CurrentPrimary != podName {
		// Nothing to be done here, since I'm not the primary server
		return
	}

	var options []string
	if cluster.Spec.Backup.Wal != nil {
		if len(cluster.Spec.Backup.Wal.Compression) != 0 {
			options = append(
				options,
				fmt.Sprintf("--%v", cluster.Spec.Backup.Wal.Compression))
		}
		if len(cluster.Spec.Backup.Wal.Encryption) != 0 {
			options = append(
				options,
				"-e",
				string(cluster.Spec.Backup.Wal.Encryption))
		}
	}
	serverName := clusterName
	if len(cluster.Spec.Backup.ServerName) != 0 {
		serverName = cluster.Spec.Backup.ServerName
	}
	options = append(
		options,
		cluster.Spec.Backup.DestinationPath,
		serverName,
		flag.Arg(0))

	cmd := exec.Command("barman-cloud-wal-archive", options...) // #nosec G204
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Log.Error(err, "Error while running barman-cloud-wal-archive",
			"walName", walName,
			"pod", podName,
			"cluster", clusterName,
			"namespace", namespace,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		os.Exit(1)
	}
}
