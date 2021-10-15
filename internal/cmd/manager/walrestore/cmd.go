/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package walrestore implement the walrestore command
package walrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/blang/semver"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	cacheClient "github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache/client"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

var (
	// ErrPrimaryServer is returned when the instance is the cluster's primary, therefore doesn't need wal-restore
	ErrPrimaryServer = errors.New("avoiding restoring WAL on the primary server")
	// ErrNoBackupConfigured is returned when no backup is configured
	ErrNoBackupConfigured = errors.New("backup not configured")
)

const barmanCloudWalRestoreName = "barman-cloud-wal-restore"

// NewCmd creates a new cobra command
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var podName string

	cmd := cobra.Command{
		Use:           "wal-restore [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			contextLog := log.WithName("wal-restore")
			err := run(contextLog, namespace, clusterName, podName, args)
			if err != nil {
				if errors.Is(err, ErrNoBackupConfigured) {
					contextLog.Info("tried restoring WALs, but no backup was configured")
				} else {
					contextLog.Error(err, "failed to run wal-restore command")
				}
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

func run(contextLog log.Logger, namespace, clusterName, podName string, args []string) error {
	ctx := context.Background()

	walName := args[0]
	destinationPath := args[1]

	var cluster *apiv1.Cluster
	var err error
	var typedClient client.Client

	typedClient, err = management.NewControllerRuntimeClient()
	if err != nil {
		contextLog.Error(err, "Error while creating k8s client")
		return err
	}

	cluster, err = cacheClient.GetCluster(ctx, typedClient, namespace, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	recoverClusterName, barmanConfiguration, err := GetRecoverConfiguration(cluster, podName)
	if err != nil {
		contextLog.Error(err, "while getting recover configuration")
		return err
	}

	if barmanConfiguration == nil {
		// Backup not configured, skipping WAL
		contextLog.Trace("Skipping WAL restore, there is no backup configuration",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		return ErrNoBackupConfigured
	}
	version, err := barman.GetBarmanCloudVersion(barmanCloudWalRestoreName)
	if err != nil {
		contextLog.Error(err, "while getting barman-cloud-wal-restore version")
	}

	options, err := barmanCloudWalRestoreOptions(
		barmanConfiguration, recoverClusterName, walName, destinationPath, version)
	if err != nil {
		contextLog.Error(err, "while getting barman-cloud-wal-restore version")
		return err
	}

	env, err := cacheClient.GetEnv(ctx,
		typedClient,
		cluster.Namespace,
		barmanConfiguration,
		cache.WALRestoreKey)
	if err != nil {
		return fmt.Errorf("failed to get envs: %w", err)
	}

	barmanCloudWalRestoreCmd := exec.Command(barmanCloudWalRestoreName, options...) // #nosec G204
	barmanCloudWalRestoreCmd.Env = env
	err = execlog.RunStreaming(barmanCloudWalRestoreCmd, barmanCloudWalRestoreName)
	if err != nil {
		contextLog.Info("Error invoking "+barmanCloudWalRestoreName,
			"error", err.Error(),
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
			"options", options,
			"exitCode", barmanCloudWalRestoreCmd.ProcessState.ExitCode(),
		)
		return fmt.Errorf("unexpected failure invoking %s: %w", barmanCloudWalRestoreName, err)
	}

	return nil
}

// GetRecoverConfiguration get the appropriate recover Configuration for a given cluster
func GetRecoverConfiguration(
	cluster *apiv1.Cluster,
	podName string,
) (
	string,
	*apiv1.BarmanObjectStoreConfiguration,
	error,
) {
	recoverClusterName := cluster.Name
	var barmanConfiguration *apiv1.BarmanObjectStoreConfiguration

	switch {
	case !cluster.IsReplica() && cluster.Status.CurrentPrimary == podName:
		// Why a request to restore a WAL file is arriving from the primary server?
		// Something strange is happening here
		return "", nil, ErrPrimaryServer

	case cluster.IsReplica() && cluster.Status.CurrentPrimary == podName:
		// I am the designated primary. Let's use the recovery object store for this wal
		sourceName := cluster.Spec.ReplicaCluster.Source
		externalCluster, found := cluster.ExternalCluster(sourceName)
		if !found {
			return "", nil, fmt.Errorf("external cluster not found: %v", sourceName)
		}

		barmanConfiguration = externalCluster.BarmanObjectStore
		recoverClusterName = externalCluster.Name

	default:
		// I am a plain replica. Let's use the object store which we are using to
		// back up this cluster
		if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
			barmanConfiguration = cluster.Spec.Backup.BarmanObjectStore
		}
	}
	return recoverClusterName, barmanConfiguration, nil
}

func barmanCloudWalRestoreOptions(
	configuration *apiv1.BarmanObjectStoreConfiguration,
	clusterName string,
	walName string,
	destinationPath string,
	version *semver.Version,
) ([]string, error) {
	var options []string

	var barmanCloudVersionGE213 bool
	if version != nil {
		barmanCloudVersionGE213 = version.GE(semver.Version{Major: 2, Minor: 13})
	}
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

	if barmanCloudVersionGE213 {
		if configuration.S3Credentials != nil {
			options = append(
				options,
				"--cloud-provider",
				"aws-s3")
		}
		if configuration.AzureCredentials != nil {
			options = append(
				options,
				"--cloud-provider",
				"azure-blob-storage")
		}
	} else if configuration.AzureCredentials != nil {
		return nil, fmt.Errorf("barman >= 2.13 is required to use Azure object storage, current: %v", version)
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
	return options, nil
}
