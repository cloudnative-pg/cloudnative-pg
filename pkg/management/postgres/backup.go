/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// BackupCommand represent a backup command that is being executed
type BackupCommand struct {
	Cluster  *apiv1.Cluster
	Backup   *apiv1.Backup
	Client   client.Client
	Recorder record.EventRecorder
	Env      []string
	Log      logr.Logger
}

// NewBackupCommand initializes a BackupCommand object
func NewBackupCommand(
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	client client.Client,
	recorder record.EventRecorder,
	log logr.Logger,
) *BackupCommand {
	return &BackupCommand{
		Cluster:  cluster,
		Backup:   backup,
		Client:   client,
		Recorder: recorder,
		Env:      os.Environ(),
		Log:      log,
	}
}

// getBarmanCloudBackupOptions extract the list of command line options to be used with
// barman-cloud-backup
func (b *BackupCommand) getBarmanCloudBackupOptions(
	configuration *apiv1.BarmanObjectStoreConfiguration, serverName string) []string {
	options := []string{
		"--user", "postgres",
	}

	if configuration.Data != nil {
		if len(configuration.Data.Compression) != 0 {
			options = append(
				options,
				fmt.Sprintf("--%v", configuration.Data.Compression))
		}

		if len(configuration.Data.Encryption) != 0 {
			options = append(
				options,
				"--encrypt",
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
	}

	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}

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

	options = append(
		options,
		configuration.DestinationPath,
		serverName)

	return options
}

// Start initiates a backup for this instance using
// barman-cloud-backup
func (b *BackupCommand) Start(ctx context.Context) error {
	b.setupBackupStatus()

	err := utils.UpdateStatusAndRetry(ctx, b.Client, b.Backup.GetKubernetesObject())
	if err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	db, err := sql.Open(
		"postgres",
		fmt.Sprintf("host=%s port=5432 dbname=postgres user=postgres sslmode=disable",
			GetSocketDir()),
	)
	if err != nil {
		log.Log.Error(err, "can not open postgres database")
		return err
	}

	walArchivingWorking := false
	for {
		row := db.QueryRow("SELECT COALESCE(last_archived_time,'-infinity') > " +
			"COALESCE(last_failed_time, '-infinity') AS is_archiving FROM pg_stat_archiver;")

		if err := row.Scan(&walArchivingWorking); err != nil {
			log.Log.Error(err, "can't get wal archiving status")
		}
		if walArchivingWorking && err == nil {
			log.Log.Info("wal archiving is working, will retry proceed with the backup")
			if b.Backup.GetStatus().Phase != apiv1.BackupPhaseRunning {
				b.Backup.GetStatus().Phase = apiv1.BackupPhaseRunning
				err := utils.UpdateStatusAndRetry(ctx, b.Client, b.Backup.GetKubernetesObject())
				if err != nil {
					log.Log.Error(err, "can't set backup as wal archiving failing")
				}
			}
			break
		}

		b.Backup.GetStatus().Phase = apiv1.BackupPhaseWalArchivingFailing
		err := utils.UpdateStatusAndRetry(ctx, b.Client, b.Backup.GetKubernetesObject())
		if err != nil {
			log.Log.Error(err, "can't set backup as wal archiving failing")
		}
		log.Log.Info("wal archiving is not working, will retry in one minute")
		time.Sleep(time.Minute * 1)
	}

	b.Env, err = barman.EnvSetCloudCredentials(
		ctx,
		b.Client,
		b.Cluster.Namespace,
		b.Cluster.Spec.Backup.BarmanObjectStore,
		b.Env)
	if err != nil {
		return fmt.Errorf("cannot recover AWS credentials: %w", err)
	}

	// Run the actual backup process
	go b.run(ctx)

	return nil
}

// run executes the barman-cloud-backup command and updates the status
// This method will take long time and is supposed to run inside a dedicated
// goroutine.
func (b *BackupCommand) run(ctx context.Context) {
	barmanConfiguration := b.Cluster.Spec.Backup.BarmanObjectStore
	backupStatus := b.Backup.GetStatus()
	options := b.getBarmanCloudBackupOptions(barmanConfiguration, backupStatus.ServerName)

	b.Log.Info("Backup started", "options", options)

	b.Recorder.Event(b.Backup, "Normal", "Starting", "Backup started")

	if err := fileutils.EnsureDirectoryExist(postgres.BackupTemporaryDirectory); err != nil {
		b.Log.Error(err, "Cannot create backup temporary directory", "err", err)
		return
	}

	const barmanCloudBackupName = "barman-cloud-backup"
	cmd := exec.Command(barmanCloudBackupName, options...) // #nosec G204
	cmd.Env = b.Env
	cmd.Env = append(cmd.Env, "TMPDIR="+postgres.BackupTemporaryDirectory)
	err := execlog.RunStreaming(cmd, barmanCloudBackupName)
	if err != nil {
		// Set the status to failed and exit
		b.Log.Error(err, "Backup failed")
		backupStatus.SetAsFailed(err)
		b.Recorder.Event(b.Backup, "Normal", "Failed", "Backup failed")
		if err := utils.UpdateStatusAndRetry(ctx, b.Client, b.Backup.GetKubernetesObject()); err != nil {
			b.Log.Error(err, "Can't mark backup as failed")
		}
		return
	}

	// Set the status to completed
	b.Log.Info("Backup completed")
	backupStatus.SetAsCompleted()
	b.Recorder.Event(b.Backup, "Normal", "Completed", "Backup completed")

	// Update status
	b.updateCompletedBackupStatus()
	if err := utils.UpdateStatusAndRetry(ctx, b.Client, b.Backup.GetKubernetesObject()); err != nil {
		b.Log.Error(err, "Can't set backup status as completed")
	}
}

// setupBackupStatus configures the backup's status from the provided configuration and instance
func (b *BackupCommand) setupBackupStatus() {
	barmanConfiguration := b.Cluster.Spec.Backup.BarmanObjectStore
	backupStatus := b.Backup.GetStatus()

	backupStatus.S3Credentials = barmanConfiguration.S3Credentials
	backupStatus.AzureCredentials = barmanConfiguration.AzureCredentials
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

// updateCompletedBackupStatus updates the backup calling barman-cloud-backup-list
// to retrieve all the relevant data
func (b *BackupCommand) updateCompletedBackupStatus() {
	backupStatus := b.Backup.GetStatus()

	// Extracting latest backup using barman-cloud-backup-list
	backupList, err := barman.GetBackupList(b.Cluster.Spec.Backup.BarmanObjectStore, backupStatus.ServerName, b.Env)
	if err != nil {
		// Proper logging already happened inside getBackupList
		return
	}

	// Update the backup with the data from the backup list retrieved
	// get latest backup and set BackupId, StartedAt, StoppedAt, BeginWal, EndWAL, BeginLSN, EndLSN
	latestBackup := backupList.LatestBackupInfo()
	backupStatus.BackupID = latestBackup.ID
	backupStatus.StartedAt = &metav1.Time{Time: latestBackup.BeginTime}
	backupStatus.StoppedAt = &metav1.Time{Time: latestBackup.EndTime}
	backupStatus.BeginWal = latestBackup.BeginWal
	backupStatus.EndWal = latestBackup.EndWal
	backupStatus.BeginLSN = latestBackup.BeginLSN
	backupStatus.EndLSN = latestBackup.EndLSN
}
