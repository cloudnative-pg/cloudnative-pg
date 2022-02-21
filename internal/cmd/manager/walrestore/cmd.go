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
	"strings"
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
	// ErrEndOfWALStreamReached is returned when end of WAL is detected in the cloud archive
	ErrEndOfWALStreamReached = errors.New("end of WAL reached")

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

			switch {
			case errors.Is(err, restorer.ErrWALNotFound):
				// Nothing to log here. The failure has already been logged.
			case errors.Is(err, ErrNoBackupConfigured):
				contextLog.Info("tried restoring WALs, but no backup was configured")
			case errors.Is(err, ErrEndOfWALStreamReached):
				contextLog.Info(
					"end-of-wal-stream flag found. " +
						"Exiting with error once to let Postgres try switching to streaming replication")
				return nil
			default:
				contextLog.Info("wal-restore command failed", "error", err)
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

	recoverClusterName, recoverEnv, barmanConfiguration, err := GetRecoverConfiguration(cluster, podName)
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

	mergeEnv(env, recoverEnv)

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

	// Step 2: return error if the end-of-wal-stream flag is set.
	// We skip this step if streaming connection is not available
	if isStreamingAvailable(cluster, podName) {
		if err := checkEndOfWALStreamFlag(walRestorer); err != nil {
			return err
		}
	}

	// Step 3: gather the WAL files names to restore. If the required file isn't a regular WAL, we download it directly.
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

	// Step 4: download the WAL files into the required place
	downloadStartTime := time.Now()
	walStatus := walRestorer.RestoreList(ctx, walFilesList, destinationPath, options)

	// We return immediately if the first WAL has errors, because the first WAL
	// is the one that PostgreSQL has requested to restore.
	// The failure has already been logged in walRestorer.RestoreList method
	if walStatus[0].Err != nil {
		return walStatus[0].Err
	}

	// Step 5: set end-of-wal-stream flag if any download job returned file-not-found
	// We skip this step if streaming connection is not available
	endOfWALStream := isEndOfWALStream(walStatus)
	if isStreamingAvailable(cluster, podName) && endOfWALStream {
		contextLog.Info(
			"Set end-of-wal-stream flag as one of the WAL files to be prefetched was not found")

		err = walRestorer.SetEndOfWALStream()
		if err != nil {
			return err
		}
	}

	successfulWalRestore := 0
	for idx := range walStatus {
		if walStatus[idx].Err == nil {
			successfulWalRestore++
		}
	}

	contextLog.Info("WAL restore command completed (parallel)",
		"walName", walName,
		"maxParallel", maxParallel,
		"successfulWalRestore", successfulWalRestore,
		"failedWalRestore", maxParallel-successfulWalRestore,
		"endOfWALStream", endOfWALStream,
		"startTime", startTime,
		"downloadStartTime", downloadStartTime,
		"downloadTotalTime", time.Since(downloadStartTime),
		"totalTime", time.Since(startTime))

	return nil
}

// checkEndOfWALStreamFlag returns ErrEndOfWALStreamReached if the flag is set in the restorer
func checkEndOfWALStreamFlag(walRestorer *restorer.WALRestorer) error {
	contain, err := walRestorer.IsEndOfWALStream()
	if err != nil {
		return err
	}

	if contain {
		err := walRestorer.ResetEndOfWalStream()
		if err != nil {
			return err
		}

		return ErrEndOfWALStreamReached
	}
	return nil
}

// isEndOfWALStream returns true if one of the downloads has returned
// a file-not-found error
func isEndOfWALStream(results []restorer.Result) bool {
	for _, result := range results {
		if errors.Is(result.Err, restorer.ErrWALNotFound) {
			return true
		}
	}

	return false
}

// mergeEnv merges all the values inside incomingEnv into env
func mergeEnv(env []string, incomingEnv []string) {
	for _, incomingItem := range incomingEnv {
		incomingKV := strings.SplitAfterN(incomingItem, "=", 2)
		if len(incomingKV) != 2 {
			continue
		}
		for idx, item := range env {
			if strings.HasPrefix(item, incomingKV[0]) {
				env[idx] = incomingItem
			}
		}
	}
}

// GetRecoverConfiguration get the appropriate recover Configuration for a given cluster
func GetRecoverConfiguration(
	cluster *apiv1.Cluster,
	podName string,
) (
	string,
	[]string,
	*apiv1.BarmanObjectStoreConfiguration,
	error,
) {
	var env []string
	// If I am the designated primary. Let's use the recovery object store for this wal
	if cluster.IsReplica() && cluster.Status.CurrentPrimary == podName {
		sourceName := cluster.Spec.ReplicaCluster.Source
		externalCluster, found := cluster.ExternalCluster(sourceName)
		if !found {
			return "", nil, nil, ErrExternalClusterNotFound
		}

		if externalCluster.BarmanObjectStore == nil {
			return "", nil, nil, ErrNoBackupConfigured
		}
		configuration := externalCluster.BarmanObjectStore
		if configuration.EndpointCA != nil && configuration.S3Credentials != nil {
			env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
		} else if configuration.EndpointCA != nil && configuration.AzureCredentials != nil {
			env = append(env, fmt.Sprintf("REQUESTS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
		}
		return externalCluster.Name, env, externalCluster.BarmanObjectStore, nil
	}

	// Otherwise, let's use the object store which we are using to
	// back up this cluster
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		configuration := cluster.Spec.Backup.BarmanObjectStore
		if configuration.EndpointCA != nil && configuration.S3Credentials != nil {
			env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanBackupEndpointCACertificateLocation))
		} else if configuration.EndpointCA != nil && configuration.AzureCredentials != nil {
			env = append(env, fmt.Sprintf("REQUESTS_CA_BUNDLE=%s", postgres.BarmanBackupEndpointCACertificateLocation))
		}
		return cluster.Name, env, cluster.Spec.Backup.BarmanObjectStore, nil
	}

	return "", nil, nil, ErrNoBackupConfigured
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

// isStreamingAvailable checks if this pod can replicate via streaming connection
func isStreamingAvailable(cluster *apiv1.Cluster, podName string) bool {
	if cluster == nil {
		return false
	}

	// Easy case: If this pod is a replica, the streaming is always available
	if cluster.Status.CurrentPrimary != podName {
		return true
	}

	// Designated primary in a replica cluster: return true if the external cluster has streaming connection
	if cluster.IsReplica() {
		externalCluster, found := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)

		// This is a configuration error
		if !found {
			return false
		}

		return externalCluster.ConnectionParameters != nil
	}

	// Primary, we do not replicate from nobody
	return false
}
