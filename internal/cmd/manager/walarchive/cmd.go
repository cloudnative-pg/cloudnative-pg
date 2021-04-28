/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package walarchive implement the wal-archive command
package walarchive

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// NewCmd create the new cobra command
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var podName string

	cmd := cobra.Command{
		Use:           "wal-archive [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(_cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			walName := args[0]

			typedClient, err := management.NewControllerRuntimeClient()
			if err != nil {
				log.Log.Error(err, "Error while creating k8s client")
				return err
			}

			var cluster apiv1.Cluster
			err = typedClient.Get(ctx, client.ObjectKey{
				Namespace: namespace,
				Name:      clusterName,
			}, &cluster)
			if err != nil {
				log.Log.Error(err, "Error while getting the cluster status")
				return err
			}

			if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
				// Backup not configured, skipping WAL
				log.Log.V(4).Info("Backup not configured, skipping WAL",
					"walName", walName,
					"pod", podName,
					"cluster", clusterName,
					"namespace", namespace,
					"currentPrimary", cluster.Status.CurrentPrimary,
					"targetPrimary", cluster.Status.TargetPrimary,
				)
				return nil
			}

			if cluster.Status.CurrentPrimary != podName {
				// Nothing to be done here, since I'm not the primary server
				return nil
			}

			configuration := cluster.Spec.Backup.BarmanObjectStore

			var options []string
			if configuration.Wal != nil {
				if len(configuration.Wal.Compression) != 0 {
					options = append(
						options,
						fmt.Sprintf("--%v", configuration.Wal.Compression))
				}
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
				walName)

			if err = postgres.SetAWSCredentials(ctx, typedClient, &cluster); err != nil {
				log.Log.Error(err, "Error while settings AWS environment variables",
					"walName", walName,
					"pod", podName,
					"cluster", clusterName,
					"namespace", namespace,
					"currentPrimary", cluster.Status.CurrentPrimary,
					"targetPrimary", cluster.Status.TargetPrimary,
					"options", options)
				return err
			}

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
					"options", options,
				)
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the "+
		"current pod in k8s")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")

	return &cmd
}
