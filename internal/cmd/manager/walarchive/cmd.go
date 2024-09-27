/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package walarchive implement the wal-archive command
package walarchive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	barmanArchiver "github.com/cloudnative-pg/barman-cloud/pkg/archiver"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	cacheClient "github.com/cloudnative-pg/cloudnative-pg/internal/management/cache/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	pgManagement "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errSwitchoverInProgress is raised when there is a switchover in progress
// and the new primary have not completed the promotion
var errSwitchoverInProgress = fmt.Errorf("switchover in progress, refusing archiving")

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	var podName string
	var pgData string
	cmd := cobra.Command{
		Use:           "wal-archive [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			const logErrorMessage = "failed to run wal-archive command"

			contextLog := log.WithName("wal-archive")
			ctx := log.IntoContext(cobraCmd.Context(), contextLog)

			if podName == "" {
				err := fmt.Errorf("no pod-name value passed and failed to extract it from POD_NAME env variable")
				contextLog.Error(err, logErrorMessage)
				return err
			}

			typedClient, err := management.NewControllerRuntimeClient()
			if err != nil {
				contextLog.Error(err, "creating controller-runtine client")
				return err
			}

			cluster, err := cacheClient.GetCluster()
			if err != nil {
				return fmt.Errorf("failed to get cluster: %w", err)
			}

			err = run(ctx, podName, pgData, cluster, args)
			if err != nil {
				if errors.Is(err, errSwitchoverInProgress) {
					contextLog.Warning("Refusing to archive WALs until the switchover is not completed",
						"err", err)
				} else {
					contextLog.Error(err, logErrorMessage)
				}

				condition := metav1.Condition{
					Type:    string(apiv1.ConditionContinuousArchiving),
					Status:  metav1.ConditionFalse,
					Reason:  string(apiv1.ConditionReasonContinuousArchivingFailing),
					Message: err.Error(),
				}
				if errCond := conditions.Patch(ctx, typedClient, cluster, &condition); errCond != nil {
					log.Error(errCond, "Error changing wal archiving condition (wal archiving failed)")
				}
				return err
			}

			// Update the condition if needed.
			condition := metav1.Condition{
				Type:    string(apiv1.ConditionContinuousArchiving),
				Status:  metav1.ConditionTrue,
				Reason:  string(apiv1.ConditionReasonContinuousArchivingSuccess),
				Message: "Continuous archiving is working",
			}
			if errCond := conditions.Patch(ctx, typedClient, cluster, &condition); errCond != nil {
				log.Error(errCond, "Error changing wal archiving condition (wal archiving succeeded)")
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the "+
		"current pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be used")

	return &cmd
}

func run(
	ctx context.Context,
	podName, pgData string,
	cluster *apiv1.Cluster,
	args []string,
) error {
	startTime := time.Now()
	contextLog := log.FromContext(ctx)
	walName := args[0]

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

	// Request the plugins to archive this WAL
	if err := archiveWALViaPlugins(ctx, cluster, path.Join(pgData, walName)); err != nil {
		return err
	}

	// Request Barman Cloud to archive this WAL
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		// Backup not configured, skipping WAL
		contextLog.Info("Backup not configured, skip WAL archiving via Barman Cloud",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary,
		)
		return nil
	}

	// Get environment from cache
	env, err := cacheClient.GetEnv(cache.WALArchiveKey)
	if err != nil {
		return fmt.Errorf("failed to get envs: %w", err)
	}

	maxParallel := 1
	if cluster.Spec.Backup.BarmanObjectStore.Wal != nil {
		maxParallel = cluster.Spec.Backup.BarmanObjectStore.Wal.MaxParallel
	}

	// Create the archiver
	var walArchiver *barmanArchiver.WALArchiver
	if walArchiver, err = barmanArchiver.New(
		ctx,
		env,
		postgres.SpoolDirectory,
		pgData,
		path.Join(pgData, pgManagement.CheckEmptyWalArchiveFile)); err != nil {
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
		contextLog.Info("Archived WAL file (parallel)",
			"walName", walName,
			"currentPrimary", cluster.Status.CurrentPrimary,
			"targetPrimary", cluster.Status.TargetPrimary)
		return nil
	}

	// Step 3: gather the WAL files names to archive
	walFilesList := walArchiver.GatherWALFilesToArchive(ctx, walName, maxParallel)

	options, err := walArchiver.BarmanCloudWalArchiveOptions(
		ctx, cluster.Spec.Backup.BarmanObjectStore, cluster.Name)
	if err != nil {
		return err
	}

	// Step 5: archive the WAL files in parallel
	uploadStartTime := time.Now()
	walStatus := walArchiver.ArchiveList(ctx, walFilesList, options)
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

// archiveWALViaPlugins requests every capable plugin to archive the passed
// WAL file, and returns an error if a configured plugin fails to do so.
// It will not return an error if there's no plugin capable of WAL archiving
func archiveWALViaPlugins(
	ctx context.Context,
	cluster *apiv1.Cluster,
	walName string,
) error {
	contextLogger := log.FromContext(ctx)

	plugins := repository.New()
	if err := plugins.RegisterUnixSocketPluginsInPath(configuration.Current.PluginSocketDir); err != nil {
		contextLogger.Error(err, "Error while loading local plugins")
	}
	defer plugins.Close()

	client, err := pluginClient.WithPlugins(ctx, plugins, cluster.Spec.Plugins.GetEnabledPluginNames()...)
	if err != nil {
		contextLogger.Error(err, "Error while loading required plugins")
		return err
	}
	defer client.Close(ctx)

	return client.ArchiveWAL(ctx, cluster, walName)
}

// isCheckWalArchiveFlagFilePresent returns true if the file CheckEmptyWalArchiveFile is present in the PGDATA directory
func isCheckWalArchiveFlagFilePresent(ctx context.Context, pgDataDirectory string) bool {
	contextLogger := log.FromContext(ctx)
	filePath := filepath.Join(pgDataDirectory, pgManagement.CheckEmptyWalArchiveFile)

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
	checkWalOptions, err := walArchiver.BarmanCloudCheckWalArchiveOptions(
		ctx, cluster.Spec.Backup.BarmanObjectStore, cluster.Name)
	if err != nil {
		log.Error(err, "while getting barman-cloud-wal-archive options")
		return err
	}

	if !isCheckWalArchiveFlagFilePresent(ctx, pgData) {
		return nil
	}

	if err := walArchiver.CheckWalArchiveDestination(ctx, checkWalOptions); err != nil {
		log.Error(err, "while barman-cloud-check-wal-archive")
		return err
	}

	return nil
}
