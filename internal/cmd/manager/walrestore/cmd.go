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

// Package walrestore implement the walrestore command
package walrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	barmanCommand "github.com/cloudnative-pg/barman-cloud/pkg/command"
	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/spf13/cobra"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/local"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
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
	var pgData string

	cmd := cobra.Command{
		Use:           "wal-restore [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			// TODO: The command is triggered by PG, resulting in the loss of stdout logs.
			// TODO: We need to implement a logpipe to prevent this.
			contextLog := log.WithName("wal-restore")
			ctx := log.IntoContext(cobraCmd.Context(), contextLog)
			err := run(ctx, pgData, podName, args)
			if err == nil {
				return nil
			}

			switch {
			case errors.Is(err, barmanRestorer.ErrWALNotFound):
				// Nothing to log here. The failure has already been logged.
			case errors.Is(err, ErrNoBackupConfigured):
				contextLog.Debug("tried restoring WALs, but no backup was configured")
			case errors.Is(err, ErrEndOfWALStreamReached):
				contextLog.Info(
					"end-of-wal-stream flag found." +
						"Exiting with error once to let Postgres try switching to streaming replication")
				return err
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
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be used")

	return &cmd
}

func run(ctx context.Context, pgData string, podName string, args []string) error {
	contextLog := log.FromContext(ctx)
	startTime := time.Now()
	walName := args[0]
	destinationPath := args[1]

	var cluster *apiv1.Cluster
	var err error

	cacheClient := local.NewClient().Cache()
	cluster, err = cacheClient.GetCluster()
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	// Validate timeline history files before any restore attempt (plugin or in-tree)
	// to ensure replicas don't download future timeline history files
	if err := validateTimelineHistoryFile(ctx, walName, cluster, podName); err != nil {
		return err
	}

	walFound, err := restoreWALViaPlugins(ctx, cluster, walName, pgData, destinationPath)
	if err != nil {
		// With the current implementation, this happens when both of the following conditions are met:
		//
		// 1. At least one CNPG-i plugin that implements the WAL service is present.
		// 2. No plugin can restore the WAL file because:
		//   a) The requested WAL could not be found
		//   b) The plugin failed in the restoration process.
		//
		// When this happens, `walFound` is false, prompting us to revert to the in-tree barman-cloud support.
		contextLog.Trace("could not restore WAL via plugins", "wal", walName, "error", err)
	}
	if walFound {
		// This happens only if a CNPG-i plugin was able to restore
		// the requested WAL.
		return nil
	}

	options, env, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName)
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
		return err
	}

	// Create the restorer
	var walRestorer *barmanRestorer.WALRestorer
	if walRestorer, err = barmanRestorer.New(ctx, env, SpoolDirectory); err != nil {
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

// restoreWALViaPlugins requests every capable plugin to restore the passed
// WAL file, and returns an error if every plugin failed. It will not return
// an error if there's no plugin capable of WAL archiving too
func restoreWALViaPlugins(
	ctx context.Context,
	cluster *apiv1.Cluster,
	walName string,
	pgData string,
	destinationPathName string,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	plugins := repository.New()
	defer plugins.Close()

	enabledPluginNames := apiv1.GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)
	enabledPluginNames = append(
		enabledPluginNames,
		apiv1.GetExternalClustersEnabledPluginNames(cluster.Spec.ExternalClusters)...,
	)
	enabledPluginNamesSet := stringset.From(enabledPluginNames)
	client, err := pluginClient.NewClient(ctx, enabledPluginNamesSet)
	if err != nil {
		contextLogger.Error(err, "Error while loading required plugins")
		return false, err
	}
	defer client.Close(ctx)

	return client.RestoreWAL(ctx, cluster, walName, postgres.BuildWALPath(pgData, destinationPathName))
}

// checkEndOfWALStreamFlag returns ErrEndOfWALStreamReached if the flag is set in the restorer
func checkEndOfWALStreamFlag(walRestorer *barmanRestorer.WALRestorer) error {
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
func isEndOfWALStream(results []barmanRestorer.Result) bool {
	for _, result := range results {
		if errors.Is(result.Err, barmanRestorer.ErrWALNotFound) {
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

// getWALRestoreSettings resolves the barman-cloud-wal-restore command-line
// options, the environment carrying the object store credentials, and the
// prefetch parallelism to use for a WAL restore. See the bootstrap-recovery
// gate below for how a Job-populated cache fits into this.
func getWALRestoreSettings(
	ctx context.Context,
	cacheClient local.CacheClient,
	cluster *apiv1.Cluster,
	podName string,
) (options []string, env []string, maxParallel int, err error) {
	// During a bootstrap recovery no primary has been elected yet and there is no
	// instance-manager cache. The Job resolves the recovery source object store
	// (for a recovery.backup reference it lives only in the referenced Backup CR,
	// so it is not derivable from the cluster spec here) and caches it for us.
	// Gating on CurrentPrimary keeps this lookup off a running instance's hot
	// path, where the key is never set.
	//
	// This gate relies on CurrentPrimary never reverting to empty once set.
	// If that ever stopped being true, a long-running instance could
	// re-enter this branch and, on a cache miss, silently fall through to
	// GetRecoverConfiguration below instead of hitting its intended hot-path
	// resolution.
	if cluster.Status.CurrentPrimary == "" {
		barmanConfiguration, configErr := cacheClient.GetBarmanObjectStore(cache.WALRestoreConfigKey)
		switch {
		case configErr == nil:
			// The cached recovery-source store's ServerName is always populated by
			// loadBackup (from the Backup CR, or from the external cluster's
			// GetServerName()), so passing it again here is not a real fallback.
			return walRestoreSettingsFromStore(ctx, cacheClient, barmanConfiguration, barmanConfiguration.ServerName, nil)
		case !errors.Is(configErr, cache.ErrCacheMiss):
			// A real failure resolving the bootstrap-cached recovery source store
			// must not be mistaken for "nothing cached yet": falling through here
			// would silently restore WALs from the cluster's own (unrelated, and
			// possibly empty) backup destination instead of the recovery source.
			return nil, nil, 0, fmt.Errorf("while getting the cached recovery source object store: %w", configErr)
		}
	}

	recoverClusterName, recoverEnv, barmanConfiguration, err := GetRecoverConfiguration(cluster, podName)
	if err != nil {
		if errors.Is(err, ErrNoBackupConfigured) {
			// returned unwrapped so the caller can detect it and skip the WAL
			return nil, nil, 0, err
		}
		return nil, nil, 0, fmt.Errorf("while getting recover configuration: %w", err)
	}

	return walRestoreSettingsFromStore(ctx, cacheClient, barmanConfiguration, recoverClusterName, recoverEnv)
}

// walRestoreSettingsFromStore builds the barman-cloud-wal-restore options, loads
// the credentials environment, and computes the prefetch parallelism from a
// resolved object store configuration. recoverEnv carries extra environment
// (e.g. the endpoint CA bundle location) to merge on top of the cached
// credentials; it is nil for a bootstrap recovery, whose cached credentials
// already include it.
func walRestoreSettingsFromStore(
	ctx context.Context,
	cacheClient local.CacheClient,
	barmanConfiguration *apiv1.BarmanObjectStoreConfiguration,
	serverName string,
	recoverEnv []string,
) (options []string, env []string, maxParallel int, err error) {
	options, err = barmanCommand.CloudWalRestoreOptions(ctx, barmanConfiguration, serverName)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("while getting barman-cloud-wal-restore options: %w", err)
	}

	env, err = cacheClient.GetEnv(cache.WALRestoreKey)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to get envs: %w", err)
	}
	mergeEnv(env, recoverEnv)

	maxParallel = 1
	if barmanConfiguration.Wal != nil && barmanConfiguration.Wal.MaxParallel > 1 {
		maxParallel = barmanConfiguration.Wal.MaxParallel
	}

	return options, env, maxParallel, nil
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
		if configuration.EndpointCA != nil && configuration.AWS != nil {
			env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
		} else if configuration.EndpointCA != nil && configuration.Azure != nil {
			env = append(env, fmt.Sprintf("REQUESTS_CA_BUNDLE=%s", postgres.BarmanRestoreEndpointCACertificateLocation))
		}
		return externalCluster.Name, env, externalCluster.BarmanObjectStore, nil
	}

	// Otherwise, let's use the object store which we are using to
	// back up this cluster
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		configuration := cluster.Spec.Backup.BarmanObjectStore
		if configuration.EndpointCA != nil && configuration.AWS != nil {
			env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", postgres.BarmanBackupEndpointCACertificateLocation))
		} else if configuration.EndpointCA != nil && configuration.Azure != nil {
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

// isStreamingAvailable checks if this pod can replicate via streaming connection
func isStreamingAvailable(cluster *apiv1.Cluster, podName string) bool {
	if cluster == nil {
		return false
	}

	// Easy case take 1: we are helping PostgreSQL to create the first
	// instance of a Cluster. No streaming connection is possible.
	if cluster.Status.CurrentPrimary == "" {
		return false
	}

	// Easy case take 2: If this pod is a replica, the streaming is always
	// available
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

// validateTimelineHistoryFile prevents replicas from downloading timeline history files
// for timelines ahead of the cluster's current timeline. Primaries can download any timeline.
func validateTimelineHistoryFile(ctx context.Context, walName string, cluster *apiv1.Cluster, podName string) error {
	contextLog := log.FromContext(ctx)

	if !strings.HasSuffix(walName, ".history") {
		return nil
	}

	fileTimeline, err := postgres.ParseTimelineFromHistoryFilename(walName)
	if err != nil {
		contextLog.Warning("Could not parse timeline from history filename",
			"walName", walName,
			"error", err)
		return nil
	}

	if cluster.Status.CurrentPrimary == podName || cluster.Status.TargetPrimary == podName {
		contextLog.Trace("Allowing timeline history file download for primary",
			"walName", walName,
			"fileTimeline", fileTimeline,
			"isPrimary", true)
		return nil
	}

	clusterTimeline := cluster.Status.TimelineID

	// clusterTimeline == 0 means no primary has promoted yet and TimelineID has
	// never been written to the cluster status, as happens during bootstrap
	// recovery from an external archive. The split-brain scenario this guard
	// protects against requires an active primary, which cannot exist while
	// TimelineID is unset, so allowing all history files here is safe.
	if clusterTimeline == 0 {
		contextLog.Trace("Allowing timeline history file: cluster timeline not yet established",
			"walName", walName,
			"fileTimeline", fileTimeline)
		return nil
	}

	if fileTimeline > clusterTimeline {
		contextLog.Warning("Refusing to restore future timeline history file",
			"walName", walName,
			"fileTimeline", fileTimeline,
			"clusterTimeline", clusterTimeline)

		return barmanRestorer.ErrWALNotFound
	}

	contextLog.Trace("Allowing timeline history file download for replica",
		"walName", walName,
		"fileTimeline", fileTimeline,
		"clusterTimeline", clusterTimeline)

	return nil
}
