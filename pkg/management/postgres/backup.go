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

package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	barmanCredentials "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/credentials"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/catalog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	// this is needed to correctly open the sql connection with the pgx driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

// We wait up to 10 minutes to have a WAL archived correctly
var retryUntilWalArchiveWorking = wait.Backoff{
	Duration: 60 * time.Second,
	Steps:    10,
}

// BackupCommand represent a backup command that is being executed
type BackupCommand struct {
	Cluster      *apiv1.Cluster
	Backup       *apiv1.Backup
	BackupName   string
	Client       client.Client
	Recorder     record.EventRecorder
	Env          []string
	Log          log.Logger
	Instance     *Instance
	Capabilities *barmanCapabilities.Capabilities
}

// NewBackupCommand initializes a BackupCommand object
func NewBackupCommand(
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	client client.Client,
	recorder record.EventRecorder,
	instance *Instance,
	log log.Logger,
) (*BackupCommand, error) {
	capabilities, err := barmanCapabilities.CurrentCapabilities()
	if err != nil {
		return nil, err
	}

	var backupName string
	if capabilities.ShouldExecuteBackupWithName(cluster) {
		backupName = fmt.Sprintf("%s-%v", backup.Name, time.Now().Unix())
	} else {
		backupName = "N/A"
	}

	return &BackupCommand{
		Cluster:      cluster,
		Backup:       backup,
		Client:       client,
		Recorder:     recorder,
		BackupName:   backupName,
		Env:          os.Environ(),
		Instance:     instance,
		Log:          log,
		Capabilities: capabilities,
	}, nil
}

// getDataConfiguration gets the configuration in the `Data` object of the Barman configuration
func getDataConfiguration(
	options []string,
	configuration *apiv1.BarmanObjectStoreConfiguration,
	capabilities *barmanCapabilities.Capabilities,
) ([]string, error) {
	if configuration.Data == nil {
		return options, nil
	}

	if configuration.Data.Compression == apiv1.CompressionTypeSnappy && !capabilities.HasSnappy {
		return nil, fmt.Errorf("snappy compression is not supported in Barman %v", capabilities.Version)
	}

	if len(configuration.Data.Compression) != 0 {
		options = append(
			options,
			fmt.Sprintf("--%v", configuration.Data.Compression))
	}

	if len(configuration.Data.Encryption) != 0 {
		options = append(
			options,
			"--encryption",
			string(configuration.Data.Encryption))
	}

	if configuration.Data.ImmediateCheckpoint {
		options = append(
			options,
			"--immediate-checkpoint")
	}

	if configuration.Data.Jobs != nil {
		options = append(
			options,
			"--jobs",
			strconv.Itoa(int(*configuration.Data.Jobs)))
	}

	return options, nil
}

// getBarmanCloudBackupOptions extract the list of command line options to be used with
// barman-cloud-backup
func (b *BackupCommand) getBarmanCloudBackupOptions(
	configuration *apiv1.BarmanObjectStoreConfiguration,
	serverName string,
) ([]string, error) {
	options := []string{
		"--user", "postgres",
	}

	if b.Capabilities.ShouldExecuteBackupWithName(b.Cluster) {
		options = append(options, "--name", b.BackupName)
	}

	options, err := getDataConfiguration(options, configuration, b.Capabilities)
	if err != nil {
		return nil, err
	}

	if len(configuration.Tags) > 0 {
		tags, err := utils.MapToBarmanTagsFormat("--tags", configuration.Tags)
		if err != nil {
			return nil, err
		}
		options = append(options, tags...)
	}

	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}

	options, err = barman.AppendCloudProviderOptionsFromConfiguration(options, configuration)
	if err != nil {
		return nil, err
	}

	options = append(
		options,
		configuration.DestinationPath,
		serverName)

	return options, nil
}

// Start initiates a backup for this instance using
// barman-cloud-backup
func (b *BackupCommand) Start(ctx context.Context) error {
	if err := b.ensureBarmanCompatibility(); err != nil {
		return err
	}

	b.setupBackupStatus()

	err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup)
	if err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	if err := ensureWalArchiveIsWorking(b.Instance); err != nil {
		log.Warning("WAL archiving is not working", "err", err)
		b.Backup.GetStatus().Phase = apiv1.BackupPhaseWalArchivingFailing
		return PatchBackupStatusAndRetry(ctx, b.Client, b.Backup)
	}

	if b.Backup.GetStatus().Phase != apiv1.BackupPhaseRunning {
		b.Backup.GetStatus().Phase = apiv1.BackupPhaseRunning
		err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup)
		if err != nil {
			log.Error(err, "can't set backup as WAL archiving failing")
		}
	}

	b.Env, err = barmanCredentials.EnvSetBackupCloudCredentials(
		ctx,
		b.Client,
		b.Cluster.Namespace,
		b.Cluster.Spec.Backup.BarmanObjectStore,
		b.Env)
	if err != nil {
		return fmt.Errorf("cannot recover backup credentials: %w", err)
	}

	// Run the actual backup process
	go b.run(ctx)

	return nil
}

func (b *BackupCommand) ensureBarmanCompatibility() error {
	postgresVers, err := b.Instance.GetPgVersion()
	if err != nil {
		return err
	}
	switch {
	case postgresVers.Major == 15 && b.Capabilities.Version.Major < 3:
		return fmt.Errorf(
			"PostgreSQL %d is not supported by Barman %d.x",
			postgresVers.Major,
			b.Capabilities.Version.Major,
		)
	default:
		return nil
	}
}

func (b *BackupCommand) retryWithRefreshedCluster(
	ctx context.Context,
	cb func() error,
) error {
	return retry.OnError(retry.DefaultBackoff, resources.RetryAlways, func() error {
		if err := b.Client.Get(ctx, types.NamespacedName{
			Namespace: b.Cluster.Namespace,
			Name:      b.Cluster.Name,
		}, b.Cluster); err != nil {
			return err
		}

		return cb()
	})
}

// run executes the barman-cloud-backup command and updates the status
// This method will take long time and is supposed to run inside a dedicated
// goroutine.
func (b *BackupCommand) run(ctx context.Context) {
	if err := b.takeBackup(ctx); err != nil {
		backupStatus := b.Backup.GetStatus()

		// record the failure
		b.Log.Error(err, "Backup failed")
		b.Recorder.Event(b.Backup, "Normal", "Failed", "Backup failed")

		// update backup status as failed
		backupStatus.SetAsFailed(err)
		if err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup); err != nil {
			b.Log.Error(err, "Can't mark backup as failed")
			// We do not terminate here because we still want to do the maintenance
			// activity on the backups and to set the condition on the cluster.
		}

		// add backup failed condition to the cluster
		if failErr := b.retryWithRefreshedCluster(ctx, func() error {
			origCluster := b.Cluster.DeepCopy()

			meta.SetStatusCondition(&b.Cluster.
				Status.Conditions, metav1.Condition{
				Type:    string(apiv1.ConditionBackup),
				Status:  metav1.ConditionFalse,
				Reason:  string(apiv1.ConditionReasonLastBackupFailed),
				Message: err.Error(),
			})

			b.Cluster.Status.LastFailedBackup = utils.GetCurrentTimestampWithFormat(time.RFC3339)
			return b.Client.Status().Patch(ctx, b.Cluster, client.MergeFrom(origCluster))
		}); failErr != nil {
			b.Log.Error(failErr, "while setting cluster condition for failed backup")
			// We do not terminate here because it's more important to properly handle
			// the backup maintenance activity than putting a condition in the cluster
		}
	}

	b.backupMaintenance(ctx)
}

func (b *BackupCommand) takeBackup(ctx context.Context) error {
	barmanConfiguration := b.Cluster.Spec.Backup.BarmanObjectStore
	backupStatus := b.Backup.GetStatus()

	options, backupErr := b.getBarmanCloudBackupOptions(barmanConfiguration, backupStatus.ServerName)
	if backupErr != nil {
		b.Log.Error(backupErr, "while getting barman-cloud-backup options")
		return backupErr
	}

	// record the backup beginning
	b.Log.Info("Backup started", "options", options)
	b.Recorder.Event(b.Backup, "Normal", "Starting", "Backup started")

	// Update backup status in cluster conditions on startup
	if err := b.retryWithRefreshedCluster(ctx, func() error {
		return conditions.Patch(ctx, b.Client, b.Cluster, &metav1.Condition{
			Type:    string(apiv1.ConditionBackup),
			Status:  metav1.ConditionFalse,
			Reason:  string(apiv1.ConditionBackupStarted),
			Message: "New Backup starting up",
		})
	}); err != nil {
		b.Log.Error(err, "Error changing backup condition (backup started)")
		// We do not terminate here because we could still have a good backup
		// even if we are unable to communicate with the Kubernetes API server
	}

	if err := fileutils.EnsureDirectoryExists(postgres.BackupTemporaryDirectory); err != nil {
		b.Log.Error(err, "Cannot create backup temporary directory", "err", err)
		return err
	}

	cmd := exec.Command(barmanCapabilities.BarmanCloudBackup, options...) // #nosec G204
	cmd.Env = b.Env
	cmd.Env = append(cmd.Env, "TMPDIR="+postgres.BackupTemporaryDirectory)
	if err := execlog.RunStreaming(cmd, barmanCapabilities.BarmanCloudBackup); err != nil {
		return err
	}

	b.Log.Info("Backup completed")
	b.Recorder.Event(b.Backup, "Normal", "Completed", "Backup completed")

	// Set the status to completed
	b.Backup.Status.SetAsCompleted()

	barmanBackup, err := b.getExecutedBackupInfo(ctx)
	if err != nil {
		return err
	}

	b.Log.Debug("extracted barman backup", "backup", barmanBackup)
	assignBarmanBackupToBackup(b.Backup, b.BackupName, barmanBackup)

	if err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup); err != nil {
		b.Log.Error(err, "Can't set backup status as completed")
	}

	// Update backup status in cluster conditions on backup completion
	if err := b.retryWithRefreshedCluster(ctx, func() error {
		return conditions.Patch(ctx, b.Client, b.Cluster, &metav1.Condition{
			Type:    string(apiv1.ConditionBackup),
			Status:  metav1.ConditionTrue,
			Reason:  string(apiv1.ConditionReasonLastBackupSucceeded),
			Message: "Backup was successful",
		})
	}); err != nil {
		b.Log.Error(err, "Can't update the cluster with the completed backup data")
	}

	return nil
}

func (b *BackupCommand) getExecutedBackupInfo(
	ctx context.Context,
) (*catalog.BarmanBackup, error) {
	if b.Capabilities.ShouldExecuteBackupWithName(b.Cluster) {
		return barman.GetBackupByName(
			ctx,
			b.BackupName,
			b.Backup.Status.ServerName,
			b.Cluster.Spec.Backup.BarmanObjectStore,
			b.Env,
		)
	}
	// we don't know the id or the name of the executed backup so it fetches the last executed barman backup.
	// it could create issues in case of concurrent backups. It is a deprecated way of detecting the backup.
	return barman.GetLatestBackup(
		ctx,
		b.Backup.Status.ServerName,
		b.Cluster.Spec.Backup.BarmanObjectStore,
		b.Env,
	)
}

func (b *BackupCommand) backupMaintenance(ctx context.Context) {
	// Delete backups per policy
	if b.Cluster.Spec.Backup.RetentionPolicy != "" {
		b.Log.Info("Applying backup retention policy",
			"retentionPolicy", b.Cluster.Spec.Backup.RetentionPolicy)
		if err := barman.DeleteBackupsByPolicy(ctx, b.Cluster.Spec.Backup, b.Backup.Status.ServerName, b.Env); err != nil {
			// Proper logging already happened inside DeleteBackupsByPolicy
			b.Recorder.Event(b.Cluster, "Warning", "RetentionPolicyFailed", "Retention policy failed")
			// We do not want to return here, we must go on to set the fist recoverability point
		}
	}

	// Extracting the latest backup using barman-cloud-backup-list
	backupList, err := barman.GetBackupList(
		ctx,
		b.Cluster.Spec.Backup.BarmanObjectStore,
		b.Backup.Status.ServerName,
		b.Env,
	)
	if err != nil {
		// Proper logging already happened inside GetBackupList
		return
	}

	if err := barman.DeleteBackupsNotInCatalog(ctx, b.Client, b.Cluster, backupList); err != nil {
		b.Log.Error(err, "while deleting Backups not present in the catalog")
	}

	if err := b.retryWithRefreshedCluster(ctx, func() error {
		origCluster := b.Cluster.DeepCopy()

		// Set the first recoverability point
		if ts := backupList.FirstRecoverabilityPoint(); ts != nil {
			firstRecoverabilityPoint := ts.Format(time.RFC3339)
			b.Cluster.Status.FirstRecoverabilityPoint = firstRecoverabilityPoint
			lastBackup := backupList.LatestBackupInfo()
			if lastBackup != nil {
				b.Cluster.Status.LastSuccessfulBackup = lastBackup.EndTime.Format(time.RFC3339)
			}
		}

		return b.Client.Status().Patch(ctx, b.Cluster, client.MergeFrom(origCluster))
	}); err != nil {
		b.Log.Error(err, "while setting the firstRecoverabilityPoint and latestSuccessfulBackup")
	}
}

// PatchBackupStatusAndRetry updates a certain backup's status in the k8s database,
// retries when error occurs
func PatchBackupStatusAndRetry(
	ctx context.Context,
	cli client.Client,
	backup *apiv1.Backup,
) error {
	return retry.OnError(retry.DefaultBackoff, resources.RetryAlways,
		func() error {
			newBackup := &apiv1.Backup{}
			namespacedName := types.NamespacedName{Namespace: backup.GetNamespace(), Name: backup.GetName()}
			err := cli.Get(ctx, namespacedName, newBackup)
			if err != nil {
				return err
			}

			origBackup := newBackup.DeepCopy()

			newBackup.Status = backup.Status
			return cli.Status().Patch(ctx, newBackup, client.MergeFrom(origBackup))
		})
}

// setupBackupStatus configures the backup's status from the provided configuration and instance
func (b *BackupCommand) setupBackupStatus() {
	barmanConfiguration := b.Cluster.Spec.Backup.BarmanObjectStore
	backupStatus := b.Backup.GetStatus()

	backupStatus.BarmanCredentials = barmanConfiguration.BarmanCredentials
	backupStatus.EndpointCA = barmanConfiguration.EndpointCA
	backupStatus.EndpointURL = barmanConfiguration.EndpointURL
	backupStatus.DestinationPath = barmanConfiguration.DestinationPath
	if barmanConfiguration.Data != nil {
		backupStatus.Encryption = string(barmanConfiguration.Data.Encryption)
	}
	// Set the barman server name as specified by the user.
	// If not explicitly configured use the cluster name
	backupStatus.ServerName = barmanConfiguration.ServerName
	if backupStatus.ServerName == "" {
		backupStatus.ServerName = b.Cluster.Name
	}
	backupStatus.Phase = apiv1.BackupPhaseRunning
}

func assignBarmanBackupToBackup(backup *apiv1.Backup, backupName string, barmanBackup *catalog.BarmanBackup) {
	backupStatus := backup.GetStatus()

	backupStatus.BackupID = barmanBackup.ID
	backupStatus.BackupName = backupName
	backupStatus.StartedAt = &metav1.Time{Time: barmanBackup.BeginTime}
	backupStatus.StoppedAt = &metav1.Time{Time: barmanBackup.EndTime}
	backupStatus.BeginWal = barmanBackup.BeginWal
	backupStatus.EndWal = barmanBackup.EndWal
	backupStatus.BeginLSN = barmanBackup.BeginLSN
	backupStatus.EndLSN = barmanBackup.EndLSN
}
