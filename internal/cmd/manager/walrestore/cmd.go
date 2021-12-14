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
	"time"

	"github.com/spf13/cobra"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	cacheClient "github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache/client"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/restorer"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

var (
	// ErrNoBackupConfigured is returned when no backup is configured
	ErrNoBackupConfigured = errors.New("backup not configured")
	// ErrExternalClusterNotFound is returned when the specification refers to
	// an external cluster which is not defined. This should be prevented
	// from the validation webhook
	ErrExternalClusterNotFound = errors.New("external cluster not found")
)

const (
	// SpoolDirectory is the directory where we spool the WAL files that
	// were pre-archived in parallel
	SpoolDirectory = postgres.ScratchDataDirectory + "/wal-restore-spool"
)

// NewCmd creates a new cobra command
func NewCmd() *cobra.Command {
	var podName string

	cmd := cobra.Command{
		Use:           "wal-restore [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			contextLog := log.WithName("wal-restore")
			ctx := log.IntoContext(cobraCmd.Context(), contextLog)
			err := run(ctx, podName, args)
			if err == nil {
				return nil
			}

			if errors.Is(err, ErrNoBackupConfigured) {
				contextLog.Info("tried restoring WALs, but no backup was configured")
			} else {
				contextLog.Error(err, "failed to run wal-restore command")
			}
			contextLog.Debug("There was an error in the previous wal-restore command. Waiting 100 ms before retrying.")
			time.Sleep(100 * time.Millisecond)
			return err
		},
	}

	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the "+
		"current pod in k8s")

	return &cmd
}

func run(ctx context.Context, podName string, args []string) error {
	contextLog := log.FromContext(ctx)
	startTime := time.Now()
	walName := args[0]
	destinationPath := args[1]

	var cluster *apiv1.Cluster
	var err error

	cluster, err = cacheClient.GetCluster()
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	recoverClusterName, barmanConfiguration, err := GetRecoverConfiguration(cluster, podName)
	if errors.Is(err, ErrNoBackupConfigured) {
		// Backup not configured, skipping WAL
		contextLog.Trace("Skipping WAL restore, there is no backup configuration",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		return err
	}
	if err != nil {
		return fmt.Errorf("while getting recover configuration: %w", err)
	}

	options, err := barmanCloudWalRestoreOptions(
		barmanConfiguration, recoverClusterName)
	if err != nil {
		return fmt.Errorf("while getting barman-cloud-wal-restore options: %w", err)
	}

	env, err := cacheClient.GetEnv(cache.WALRestoreKey)
	if err != nil {
		return fmt.Errorf("failed to get envs: %w", err)
	}

	// Create the restorer
	var walRestorer *restorer.WALRestorer
	if walRestorer, err = restorer.New(ctx, cluster, env, SpoolDirectory); err != nil {
		return fmt.Errorf("while creating the restorer: %w", err)
	}

	// Step 1: check if this WAL file is not already in the spool
	var wasInSpool bool
	if wasInSpool, err = walRestorer.RestoreFromSpool(walName, destinationPath); err != nil {
		return fmt.Errorf("while restoring a file from the spool directory: %w", err)
	}
	if wasInSpool {
		contextLog.Info("Restored WAL file from spool (parallel)",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		return nil
	}

	// Step 2: gather the WAL files names to restore
	// is not really a WAL.
	// To do that we need to implement something like this function
	// here: https://github.com/EnterpriseDB/barman/blob/749f33bcfdd6ea865390ddb34ece95301c797690/barman/xlog.py#L135
	var walFilesList []string
	maxParallel := 1
	if barmanConfiguration.Wal != nil && barmanConfiguration.Wal.MaxParallel > 1 {
		maxParallel = barmanConfiguration.Wal.MaxParallel
	}
	if postgres.IsWALFile(walName) {
		// If this is a regular WAL file, we try to prefetch
		if walFilesList, err = gatherWALFilesToRestore(walName, maxParallel); err != nil {
			return fmt.Errorf("while generating the list of WAL files to restore: %w", err)
		}
	} else {
		// This is not a regular WAL file, we fetch it directly
		walFilesList = []string{walName}
	}

	// Step 3: download the WAL files into the required place
	downloadStartTime := time.Now()
	walStatus := walRestorer.RestoreList(ctx, walFilesList, destinationPath, options)
	if len(walStatus) > 1 {
		successfulWalRestore := 0
		for idx := range walStatus {
			if walStatus[idx].Err == nil {
				successfulWalRestore++
			}
		}
		contextLog.Info("Completed restore command (parallel)",
			"maxParallel", maxParallel,
			"successfulWalRestore", successfulWalRestore,
			"failedWalRestore", maxParallel-successfulWalRestore,
			"startTime", startTime,
			"downloadStartTime", downloadStartTime,
			"downloadTotalTime", time.Since(downloadStartTime),
			"totalTime", time.Since(startTime))
	}

	// We return only the first error to PostgreSQL, because the first error
	// is the one raised by the file that PostgreSQL has requested to restore.
	// The other errors are related to WAL files that were pre-restored in
	// the spool as a performance optimization and are just logged
	return walStatus[0].Err
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
	// If I am the designated primary. Let's use the recovery object store for this wal
	if cluster.IsReplica() && cluster.Status.CurrentPrimary == podName {
		sourceName := cluster.Spec.ReplicaCluster.Source
		externalCluster, found := cluster.ExternalCluster(sourceName)
		if !found {
			return "", nil, ErrExternalClusterNotFound
		}

		if externalCluster.BarmanObjectStore == nil {
			return "", nil, ErrNoBackupConfigured
		}

		return externalCluster.Name, externalCluster.BarmanObjectStore, nil
	}

	// Otherwise, let's use the object store which we are using to
	// back up this cluster
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		return cluster.Name, cluster.Spec.Backup.BarmanObjectStore, nil
	}

	return "", nil, ErrNoBackupConfigured
}

// gatherWALFilesToRestore files a list of possible WAL files to restore, always
// including as the first one the requested WAL file
func gatherWALFilesToRestore(walName string, parallel int) (walList []string, err error) {
	var segment postgres.Segment

	segment, err = postgres.SegmentFromName(walName)
	if err != nil {
		// This seems an invalid segment name. It's not a problem
		// because PostgreSQL may request also other files such as
		// backup, history, etc.
		// Let's just avoid prefetching in this case
		return []string{walName}, nil
	}
	// NextSegments would accept postgresVersion and segmentSize,
	// but we do not have this info here, so we pass nil.
	segmentList := segment.NextSegments(parallel, nil, nil)
	walList = make([]string, len(segmentList))
	for idx := range segmentList {
		walList[idx] = segmentList[idx].Name()
	}

	return walList, err
}

func barmanCloudWalRestoreOptions(
	configuration *apiv1.BarmanObjectStoreConfiguration,
	clusterName string,
) ([]string, error) {
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

	options, err := barman.AppendCloudProviderOptionsFromConfiguration(options, configuration)
	if err != nil {
		return nil, err
	}

	serverName := clusterName
	if len(configuration.ServerName) != 0 {
		serverName = configuration.ServerName
	}

	options = append(
		options,
		configuration.DestinationPath,
		serverName)
	return options, nil
}
