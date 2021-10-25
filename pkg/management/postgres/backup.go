/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/blang/semver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/catalog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

const barmanCloudBackupName = "barman-cloud-backup"

// BackupCommand represent a backup command that is being executed
type BackupCommand struct {
	Cluster  *apiv1.Cluster
	Backup   *apiv1.Backup
	Client   client.Client
	Recorder record.EventRecorder
	Env      []string
	Log      log.Logger
}

// NewBackupCommand initializes a BackupCommand object
func NewBackupCommand(
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	client client.Client,
	recorder record.EventRecorder,
	log log.Logger,
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
	configuration *apiv1.BarmanObjectStoreConfiguration, serverName string, version *semver.Version,
) ([]string, error) {
	var barmanCloudVersionGE213 bool
	if version != nil {
		barmanCloudVersionGE213 = version.GE(semver.Version{Major: 2, Minor: 13})
	}

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

	options = append(
		options,
		configuration.DestinationPath,
		serverName)

	return options, nil
}

// Start initiates a backup for this instance using
// barman-cloud-backup
func (b *BackupCommand) Start(ctx context.Context) error {
	b.setupBackupStatus()

	err := UpdateBackupStatusAndRetry(ctx, b.Client, b.Backup)
	if err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	db, err := sql.Open(
		"postgres",
		fmt.Sprintf("host=%s port=%v dbname=postgres user=postgres sslmode=disable",
			GetSocketDir(),
			GetServerPort()),
	)
	if err != nil {
		log.Error(err, "can not open postgres database")
		return err
	}

	retryUntilWalArchiveWorking := wait.Backoff{
		Duration: 60 * time.Second,
		Steps:    10,
	}

	walError := errors.New("wal-archive not working")

	err = retry.OnError(retryUntilWalArchiveWorking, func(err error) bool {
		return errors.Is(err, walError)
	}, func() error {
		row := db.QueryRow("SELECT COALESCE(last_archived_time,'-infinity') > " +
			"COALESCE(last_failed_time, '-infinity') AS is_archiving, last_failed_time IS NOT NULL " +
			"FROM pg_stat_archiver")

		var walArchivingWorking, lastFailedTimePresent bool

		if err := row.Scan(&walArchivingWorking, &lastFailedTimePresent); err != nil {
			log.Error(err, "can't get WAL archiving status")
			return err
		}

		switch {
		case walArchivingWorking:
			log.Info("WAL archiving is working, proceeding with the backup")
			return nil

		case !walArchivingWorking && !lastFailedTimePresent:
			log.Info("Waiting for the first WAL file to be archived")
			return walError

		default:
			log.Info("WAL archiving is not working, will retry in one minute")
			return walError
		}
	})
	if err != nil {
		log.Info("WAL archiving is not working")
		b.Backup.GetStatus().Phase = apiv1.BackupPhaseWalArchivingFailing
		return UpdateBackupStatusAndRetry(ctx, b.Client, b.Backup)
	}

	if b.Backup.GetStatus().Phase != apiv1.BackupPhaseRunning {
		b.Backup.GetStatus().Phase = apiv1.BackupPhaseRunning
		err := UpdateBackupStatusAndRetry(ctx, b.Client, b.Backup)
		if err != nil {
			log.Error(err, "can't set backup as WAL archiving failing")
		}
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
	version, err := barman.GetBarmanCloudVersion(barmanCloudBackupName)
	if err != nil {
		b.Log.Error(err, "while getting barman-cloud-backup version")
	}
	options, err := b.getBarmanCloudBackupOptions(barmanConfiguration, backupStatus.ServerName, version)
	if err != nil {
		b.Log.Error(err, "while getting barman-cloud-backup options")
		return
	}
	b.Log.Info("Backup started", "options", options)

	b.Recorder.Event(b.Backup, "Normal", "Starting", "Backup started")

	if err := fileutils.EnsureDirectoryExist(postgres.BackupTemporaryDirectory); err != nil {
		b.Log.Error(err, "Cannot create backup temporary directory", "err", err)
		return
	}

	cmd := exec.Command(barmanCloudBackupName, options...) // #nosec G204
	cmd.Env = b.Env
	cmd.Env = append(cmd.Env, "TMPDIR="+postgres.BackupTemporaryDirectory)
	err = execlog.RunStreaming(cmd, barmanCloudBackupName)
	if err != nil {
		// Set the status to failed and exit
		b.Log.Error(err, "Backup failed")
		backupStatus.SetAsFailed(err)
		b.Recorder.Event(b.Backup, "Normal", "Failed", "Backup failed")
		if err := UpdateBackupStatusAndRetry(ctx, b.Client, b.Backup); err != nil {
			b.Log.Error(err, "Can't mark backup as failed")
		}
		return
	}

	// Set the status to completed
	b.Log.Info("Backup completed")
	backupStatus.SetAsCompleted()
	b.Recorder.Event(b.Backup, "Normal", "Completed", "Backup completed")

	// Extracting latest backup using barman-cloud-backup-list
	backupList, err := barman.GetBackupList(b.Cluster.Spec.Backup.BarmanObjectStore, backupStatus.ServerName, b.Env)
	if err != nil || backupList.Len() == 0 {
		// Proper logging already happened inside GetBackupList
		return
	}

	// Update status
	b.updateCompletedBackupStatus(backupList)
	if err := UpdateBackupStatusAndRetry(ctx, b.Client, b.Backup); err != nil {
		b.Log.Error(err, "Can't set backup status as completed")
	}

	// For the retention policy we need Barman >= 2.14
	barmanCloudVersionGE214 := false
	if version != nil {
		barmanCloudVersionGE214 = version.GE(semver.Version{Major: 2, Minor: 14})
	}
	// Delete backups per policy
	if b.Cluster.Spec.Backup.RetentionPolicy != "" && barmanCloudVersionGE214 {
		b.Log.Info("Applying backup retention policy",
			"retentionPolicy", b.Cluster.Spec.Backup.RetentionPolicy)
		err = barman.DeleteBackupsByPolicy(b.Cluster.Spec.Backup, backupStatus.ServerName, b.Env)
		if err != nil {
			// Proper logging already happened inside DeleteBackupsByPolicy
			b.Recorder.Event(b.Cluster, "Warning", "RetentionPolicyFailed", "Retention policy failed")
			return
		}
	} else if b.Cluster.Spec.Backup.RetentionPolicy != "" && !barmanCloudVersionGE214 {
		b.Log.Info("The retention policy was detected but the current barman version is lower than 2.14")
	}

	// Set the first recoverability point
	if ts := backupList.FirstRecoverabilityPoint(); ts != nil {
		firstRecoverabilityPoint := ts.Format(time.RFC3339)
		if b.Cluster.Status.FirstRecoverabilityPoint != firstRecoverabilityPoint {
			b.Cluster.Status.FirstRecoverabilityPoint = firstRecoverabilityPoint

			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cluster := &apiv1.Cluster{}
				err := b.Client.Get(ctx,
					types.NamespacedName{Namespace: b.Cluster.GetNamespace(), Name: b.Cluster.GetName()},
					cluster)
				if err != nil {
					return err
				}
				cluster.Status.FirstRecoverabilityPoint = firstRecoverabilityPoint
				return b.Client.Status().Update(ctx, cluster)
			}); err != nil {
				b.Log.Error(err, "Can't update first recoverability point")
			}
		}
	}
}

// UpdateBackupStatusAndRetry updates a certain backup's status in the k8s database,
// retrying when conflicts are detected
func UpdateBackupStatusAndRetry(
	ctx context.Context,
	cli client.Client,
	backup *apiv1.Backup,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newBackup := &apiv1.Backup{}
		err := cli.Get(ctx, types.NamespacedName{Namespace: backup.GetNamespace(), Name: backup.GetName()}, newBackup)
		if err != nil {
			return err
		}
		newBackup.Status = backup.Status
		return cli.Status().Update(ctx, backup)
	})
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
func (b *BackupCommand) updateCompletedBackupStatus(backupList *catalog.Catalog) {
	backupStatus := b.Backup.GetStatus()

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
