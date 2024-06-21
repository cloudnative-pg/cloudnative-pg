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
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	cacheClient "github.com/cloudnative-pg/cloudnative-pg/internal/management/cache/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/archiver"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// SpoolDirectory is the directory where we spool the WAL files that
	// were pre-archived in parallel
	SpoolDirectory = postgres.ScratchDataDirectory + "/wal-archive-spool"
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
	var walArchiver *archiver.WALArchiver
	if walArchiver, err = archiver.New(ctx, cluster, env, SpoolDirectory, pgData); err != nil {
		return fmt.Errorf("while creating the archiver: %w", err)
	}

	// Step 1: check if this WAL file has not been already archived
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
	walFilesList := gatherWALFilesToArchive(ctx, walName, maxParallel)

	// Step 4: Check if the archive location is safe to perform archiving
	if utils.IsEmptyWalArchiveCheckEnabled(&cluster.ObjectMeta) {
		if err := checkWalArchive(ctx, cluster, walArchiver, pgData); err != nil {
			return err
		}
	}

	options, err := barmanCloudWalArchiveOptions(cluster, cluster.Name)
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

	pluginClient, err := cluster.LoadSelectedPluginsClient(ctx, cluster.GetWALPluginNames())
	if err != nil {
		contextLogger.Error(err, "Error loading plugins while archiving a WAL")
		return err
	}
	defer pluginClient.Close(ctx)

	return pluginClient.ArchiveWAL(ctx, cluster, walName)
}

// gatherWALFilesToArchive reads from the archived status the list of WAL files
// that can be archived in parallel way.
// `requestedWALFile` is the name of the file whose archiving was requested by
// PostgreSQL, and that file is always the first of the list and is always included.
// `parallel` is the maximum number of WALs that we can archive in parallel
func gatherWALFilesToArchive(ctx context.Context, requestedWALFile string, parallel int) (walList []string) {
	contextLog := log.FromContext(ctx)
	pgWalDirectory := path.Join(os.Getenv("PGDATA"), "pg_wal")
	archiveStatusPath := path.Join(pgWalDirectory, "archive_status")
	noMoreWALFilesNeeded := errors.New("no more files needed")

	// allocate parallel + 1 only if it does not overflow. Cap otherwise
	var walListLength int
	if parallel < math.MaxInt-1 {
		walListLength = parallel + 1
	} else {
		walListLength = math.MaxInt - 1
	}
	// slightly more optimized, but equivalent to:
	// walList = []string{requestedWALFile}
	walList = make([]string, 1, walListLength)
	walList[0] = requestedWALFile

	err := filepath.WalkDir(archiveStatusPath, func(path string, d os.DirEntry, err error) error {
		// If err is set, it means the current path is a directory and the readdir raised an error
		// The only available option here is to skip the path and log the error.
		if err != nil {
			contextLog.Error(err, "failed reading path", "path", path)
			return filepath.SkipDir
		}

		if len(walList) >= parallel {
			return noMoreWALFilesNeeded
		}

		// We don't process directories beside the archive status path
		if d.IsDir() {
			// We want to proceed exploring the archive status folder
			if path == archiveStatusPath {
				return nil
			}

			return filepath.SkipDir
		}

		// We only process ready files
		if !strings.HasSuffix(path, ".ready") {
			return nil
		}

		walFileName := strings.TrimSuffix(filepath.Base(path), ".ready")

		// We are already archiving the requested WAL file,
		// and we need to avoid archiving it twice.
		// requestedWALFile is usually "pg_wal/wal_file_name" and
		// we compare it with the path we read
		if strings.HasSuffix(requestedWALFile, walFileName) {
			return nil
		}

		walList = append(walList, filepath.Join("pg_wal", walFileName))
		return nil
	})

	// In this point err must be nil or noMoreWALFilesNeeded, if it is something different
	// there is a programming error
	if err != nil && err != noMoreWALFilesNeeded {
		contextLog.Error(err, "unexpected error while reading the list of WAL files to archive")
	}

	return walList
}

func barmanCloudWalArchiveOptions(
	cluster *apiv1.Cluster,
	clusterName string,
) ([]string, error) {
	capabilities, err := barmanCapabilities.CurrentCapabilities()
	if err != nil {
		return nil, err
	}
	configuration := cluster.Spec.Backup.BarmanObjectStore

	var options []string
	if configuration.Wal != nil {
		if configuration.Wal.Compression == apiv1.CompressionTypeSnappy && !capabilities.HasSnappy {
			return nil, fmt.Errorf("snappy compression is not supported in Barman %v", capabilities.Version)
		}
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
		options = configuration.Wal.AppendAdditionalCommandArgs(options)
	}
	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}

	if len(configuration.Tags) > 0 {
		tags, err := utils.MapToBarmanTagsFormat("--tags", configuration.Tags)
		if err != nil {
			return nil, err
		}
		options = append(options, tags...)
	}

	if len(configuration.HistoryTags) > 0 {
		historyTags, err := utils.MapToBarmanTagsFormat("--history-tags", configuration.HistoryTags)
		if err != nil {
			return nil, err
		}
		options = append(options, historyTags...)
	}

	options, err = barman.AppendCloudProviderOptionsFromConfiguration(options, configuration)
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

func checkWalArchive(ctx context.Context,
	cluster *apiv1.Cluster,
	walArchiver *archiver.WALArchiver,
	pgData string,
) error {
	checkWalOptions, err := walArchiver.BarmanCloudCheckWalArchiveOptions(cluster, cluster.Name)
	if err != nil {
		log.Error(err, "while getting barman-cloud-wal-archive options")
		return err
	}

	if !walArchiver.IsCheckWalArchiveFlagFilePresent(ctx, pgData) {
		return nil
	}

	if err := walArchiver.CheckWalArchiveDestination(ctx, checkWalOptions); err != nil {
		log.Error(err, "while barman-cloud-check-wal-archive")
		return err
	}

	return nil
}
