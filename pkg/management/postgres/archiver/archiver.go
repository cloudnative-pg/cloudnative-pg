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

package archiver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"time"

	barmanArchiver "github.com/cloudnative-pg/barman-cloud/pkg/archiver"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	walUtils "github.com/cloudnative-pg/machinery/pkg/fileutils/wals"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/local"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errSwitchoverInProgress is raised when there is a switchover in progress
// and the new primary have not completed the promotion
var errSwitchoverInProgress = fmt.Errorf("switchover in progress, refusing archiving")

// ErrMissingWALArchiverPlugin is raised when we try to archive a WAL
// file with a CNPG-i plugin whose socket does not exist.
type ErrMissingWALArchiverPlugin struct {
	// PluginName is the name of the plugin that is missing
	PluginName string
}

// Error implements the error interface
func (e ErrMissingWALArchiverPlugin) Error() string {
	return fmt.Sprintf("wal archive plugin is not available: %s", e.PluginName)
}

// ArchiveAllReadyWALs ensures that all WAL files that are in the "ready"
// queue have been archived.
// This is used to ensure that a former primary will archive the WAL files in
// its queue even in case of an unclean shutdown.
func ArchiveAllReadyWALs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pgData string,
) error {
	contextLog := log.FromContext(ctx)

	noWALLeft := errors.New("no wal files to archive")

	iterator := func() error {
		walList := walUtils.GatherReadyWALFiles(
			ctx, walUtils.GatherReadyWALFilesConfig{
				MaxResults: math.MaxInt32 - 1,
				PgDataPath: pgData,
			},
		)

		if len(walList.Ready) > 0 {
			contextLog.Info(
				"Detected ready WAL files in a former primary, triggering WAL archiving",
				"readyWALCount", len(walList.Ready),
			)
			contextLog.Debug(
				"List of ready WALs",
				"readyWALs", walList.Ready,
			)
		}

		for _, wal := range walList.ReadyItemsToSlice() {
			if err := internalRun(ctx, pgData, cluster, wal); err != nil {
				return err
			}

			if err := walList.MarkAsDone(ctx, wal); err != nil {
				return err
			}
		}

		if !walList.HasMoreResults {
			return noWALLeft
		}

		return nil
	}

	for {
		if err := iterator(); err != nil {
			if errors.Is(err, noWALLeft) {
				return nil
			}
			return err
		}
	}
}

// Run implements the WAL archiving process given the current cluster definition
// and the current Pod Name.
func Run(
	ctx context.Context,
	podName, pgData string,
	cluster *apiv1.Cluster,
	walName string,
) error {
	contextLog := log.FromContext(ctx)

	if cluster.IsReplica() {
		if podName != cluster.Status.CurrentPrimary && podName != cluster.Status.TargetPrimary {
			contextLog.Debug("WAL archiving on a replica cluster, "+
				"but this node is not the target primary nor the current one. "+
				"Skipping WAL archiving",
				"walName", walName,
				"currentPrimary", cluster.Status.CurrentPrimary,
				"targetPrimary", cluster.Status.TargetPrimary,
			)
			return nil
		}
	}

	if cluster.Status.CurrentPrimary != podName {
		contextLog.Info("Refusing to archive WAL when there is a switchover in progress",
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
			"podName", podName)
		return errSwitchoverInProgress
	}

	return internalRun(ctx, pgData, cluster, walName)
}

func internalRun(
	ctx context.Context,
	pgData string,
	cluster *apiv1.Cluster,
	walName string,
) error {
	contextLog := log.FromContext(ctx)
	startTime := time.Now()

	// We allow plugins to archive WALs even if there is no plugin
	// directly enabled by the user, to retain compatibility with
	// the old API.
	if err := archiveWALViaPlugins(ctx, cluster, pgData, walName); err != nil {
		return err
	}

	// If the used chosen a plugin to do WAL archiving, we don't
	// trigger the legacy archiving process.
	if cluster.GetEnabledWALArchivePluginName() != "" {
		return nil
	}

	// Request Barman Cloud to archive this WAL
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		// Backup not configured, skipping WAL
		contextLog.Debug("Backup not configured, skip WAL archiving via Barman Cloud",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		return nil
	}

	// Get environment from cache
	env, err := local.NewClient().Cache().GetEnv(cache.WALArchiveKey)
	if err != nil {
		return fmt.Errorf("failed to get envs: %w", err)
	}

	// Create the archiver
	var walArchiver *barmanArchiver.WALArchiver
	if walArchiver, err = barmanArchiver.New(
		ctx,
		env,
		postgres.SpoolDirectory,
		pgData,
		path.Join(pgData, constants.CheckEmptyWalArchiveFile)); err != nil {
		return fmt.Errorf("while creating the archiver: %w", err)
	}

	// Step 1: Check if the archive location is safe to perform archiving
	if utils.IsEmptyWalArchiveCheckEnabled(&cluster.ObjectMeta) {
		if err := checkWalArchive(ctx, cluster, walArchiver, pgData); err != nil {
			return err
		}
	}

	// Step 2: check if this WAL file has not been already archived
	var isDeletedFromSpool bool
	isDeletedFromSpool, err = walArchiver.DeleteFromSpool(walName)
	if err != nil {
		return fmt.Errorf("while testing the existence of the WAL file in the spool directory: %w", err)
	}
	if isDeletedFromSpool {
		contextLog.Info("WAL file already archived, skipping",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		return nil
	}

	// Step 3: gather the WAL files names to archive
	walFilesList := walUtils.GatherReadyWALFiles(
		ctx,
		walUtils.GatherReadyWALFilesConfig{
			MaxResults: getMaxResult(cluster),
			SkipWALs:   []string{walName},
			PgDataPath: pgData,
		},
	)

	// Ensure the requested WAL file is always the first one being
	// archived
	walFilesList.Ready = append([]string{walName}, walFilesList.Ready...)
	contextLog.Debug("WAL files to archive", "walFilesListReady", walFilesList.Ready)

	options, err := walArchiver.BarmanCloudWalArchiveOptions(
		ctx, cluster.Spec.Backup.BarmanObjectStore, cluster.Name)
	if err != nil {
		return err
	}

	// Step 5: archive the WAL files in parallel
	uploadStartTime := time.Now()
	walStatus := walArchiver.ArchiveList(ctx, walFilesList.ReadyItemsToSlice(), options)
	if len(walStatus) > 1 {
		contextLog.Info("Completed archive command (parallel)",
			"walsCount", len(walStatus),
			"startTime", startTime,
			"uploadStartTime", uploadStartTime,
			"uploadTotalTime", time.Since(uploadStartTime),
			"totalTime", time.Since(startTime))
	}

	// We return only the first error to PostgreSQL, because the first error
	// is the one raised by the file that PostgreSQL has requested to archive.
	// The other errors are related to WAL files that were pre-archived as
	// a performance optimization and are just logged
	return walStatus[0].Err
}

func getMaxResult(cluster *apiv1.Cluster) int {
	if cluster.Spec.Backup.BarmanObjectStore.Wal != nil && cluster.Spec.Backup.BarmanObjectStore.Wal.MaxParallel > 0 {
		return cluster.Spec.Backup.BarmanObjectStore.Wal.MaxParallel - 1
	}
	return 0
}

// archiveWALViaPlugins requests every capable plugin to archive the passed
// WAL file, and returns an error if a configured plugin fails to do so.
// It will not return an error if there's no plugin capable of WAL archiving
func archiveWALViaPlugins(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pgData string,
	walName string,
) error {
	contextLogger := log.FromContext(ctx)

	// check if the `walName` is an absolute path or just the filename
	if !filepath.IsAbs(walName) {
		walName = filepath.Join(pgData, walName)
	}

	plugins := repository.New()
	defer plugins.Close()

	enabledPluginNamesSet := stringset.From(apiv1.GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins))

	client, err := pluginClient.NewClient(ctx, enabledPluginNamesSet)
	if err != nil {
		contextLogger.Error(err, "Error while loading required plugins")
		return err
	}
	defer client.Close(ctx)

	enabledArchiverPluginName := cluster.GetEnabledWALArchivePluginName()
	if enabledArchiverPluginName != "" && !client.HasPlugin(enabledArchiverPluginName) {
		return ErrMissingWALArchiverPlugin{
			PluginName: enabledArchiverPluginName,
		}
	}

	return client.ArchiveWAL(ctx, cluster, walName)
}

// isCheckWalArchiveFlagFilePresent returns true if the file CheckEmptyWalArchiveFile is present in the PGDATA directory
func isCheckWalArchiveFlagFilePresent(ctx context.Context, pgDataDirectory string) bool {
	contextLogger := log.FromContext(ctx)
	filePath := filepath.Join(pgDataDirectory, constants.CheckEmptyWalArchiveFile)

	exists, err := fileutils.FileExists(filePath)
	if err != nil {
		contextLogger.Error(err, "error while checking for the existence of the CheckEmptyWalArchiveFile")
	}
	// If the check empty wal archive file doesn't exist this it's a no-op
	if !exists {
		contextLogger.Debug("WAL check flag file not found, skipping check")
		return false
	}

	return exists
}

func checkWalArchive(
	ctx context.Context,
	cluster *apiv1.Cluster,
	walArchiver *barmanArchiver.WALArchiver,
	pgData string,
) error {
	contextLogger := log.FromContext(ctx)
	checkWalOptions, err := walArchiver.BarmanCloudCheckWalArchiveOptions(
		ctx, cluster.Spec.Backup.BarmanObjectStore, cluster.Name)
	if err != nil {
		contextLogger.Error(err, "while getting barman-cloud-wal-archive options")
		return err
	}

	if !isCheckWalArchiveFlagFilePresent(ctx, pgData) {
		return nil
	}

	if err := walArchiver.CheckWalArchiveDestination(ctx, checkWalOptions); err != nil {
		contextLogger.Error(err, "while barman-cloud-check-wal-archive")
		return err
	}

	return nil
}
