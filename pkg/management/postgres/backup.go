/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// BackupListResult represent the result of a
// barman-cloud-backup-list invocation
type BackupListResult struct {
	// The list of backups
	List []BarmanBackup `json:"backups_list"`
}

// GetLatestBackup gets the latest backup between the ones available in the list, scanning the IDs
func (backupList *BackupListResult) GetLatestBackup() BarmanBackup {
	id := ""
	var latestBackup BarmanBackup
	for _, backup := range backupList.List {
		if backup.ID > id {
			id = backup.ID
			latestBackup = backup
		}
	}
	return latestBackup
}

// BarmanBackup represent a backup as created
// by Barman
type BarmanBackup struct {
	// The backup label
	Label string `json:"backup_label"`

	// The moment where the backup started
	BeginTime string `json:"begin_time"`

	// The moment where the backup ended
	EndTime string `json:"end_time"`

	// The WAL where the backup started
	BeginWal string `json:"begin_wal"`

	// The WAL where the backup ended
	EndWal string `json:"end_wal"`

	// The LSN where the backup started
	BeginLSN string `json:"begin_xlog"`

	// The LSN where the backup ended
	EndLSN string `json:"end_xlog"`

	// The systemID of the cluster
	SystemID string `json:"systemid"`

	// The ID of the backup
	ID string `json:"backup_id"`

	// The error output if present
	Error string `json:"error"`
}

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

// Start start a backup for this instance using
// barman-cloud-backup
func (b *BackupCommand) Start(ctx context.Context) error {
	b.setupBackupStatus()

	err := utils.UpdateStatusAndRetry(ctx, b.Client, b.Backup.GetKubernetesObject())
	if err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	b.Env, err = EnvSetCloudCredentials(ctx, b.Client, b.Cluster, b.Env)
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
	backupList, err := b.getBackupList()
	if err != nil {
		// Proper logging already happened inside getBackupList
		return
	}

	// Update the backup with the data from the backup list retrieved
	// get latest backup and set BackupId, StartedAt, StoppedAt, BeginWal, EndWAL, BeginLSN, EndLSN
	latestBackup := backupList.GetLatestBackup()
	backupStatus.BackupID = latestBackup.ID
	// date parsing layout
	layout := "Mon Jan 2 15:04:05 2006"
	started, err := time.Parse(layout, latestBackup.BeginTime)
	if err != nil {
		b.Log.Error(err, "Can't parse beginTime from latest backup")
	} else {
		startedAt := &metav1.Time{Time: started}
		backupStatus.StartedAt = startedAt
	}

	stopped, err := time.Parse(layout, latestBackup.EndTime)
	if err != nil {
		b.Log.Error(err, "Can't parse endTime from latest backup")
	} else {
		stoppedAt := &metav1.Time{Time: stopped}
		backupStatus.StoppedAt = stoppedAt
	}

	backupStatus.BeginWal = latestBackup.BeginWal
	backupStatus.EndWal = latestBackup.EndWal
	backupStatus.BeginLSN = latestBackup.BeginLSN
	backupStatus.EndLSN = latestBackup.EndLSN
}

// EnvSetCloudCredentials sets the AWS environment variables given the configuration
// inside the cluster
func EnvSetCloudCredentials(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	env []string,
) ([]string, error) {
	if cluster.Spec.Backup.BarmanObjectStore.S3Credentials != nil {
		return envSetAWSCredentials(ctx, c, cluster, env)
	}

	return envSetAzureCredentials(ctx, c, cluster, env)
}

// envSetAWSCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetAWSCredentials(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	env []string,
) ([]string, error) {
	configuration := cluster.Spec.Backup.BarmanObjectStore
	var accessKeyIDSecret corev1.Secret
	var secretAccessKeySecret corev1.Secret

	// Get access key ID
	secretName := configuration.S3Credentials.AccessKeyIDReference.Name
	secretKey := configuration.S3Credentials.AccessKeyIDReference.Key
	err := c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &accessKeyIDSecret)
	if err != nil {
		return nil, fmt.Errorf("while getting access key ID secret: %w", err)
	}

	accessKeyID, ok := accessKeyIDSecret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("missing key inside access key ID secret")
	}

	// Get secret access key
	secretName = configuration.S3Credentials.SecretAccessKeyReference.Name
	secretKey = configuration.S3Credentials.SecretAccessKeyReference.Key
	err = c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &secretAccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret access key secret: %w", err)
	}

	secretAccessKey, ok := secretAccessKeySecret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("missing key inside secret access key secret")
	}

	env = append(env, fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", accessKeyID))
	env = append(env, fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", secretAccessKey))

	return env, nil
}

// envSetAzureCredentials sets the Azure environment variables given the configuration
// inside the cluster
func envSetAzureCredentials(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	env []string,
) ([]string, error) {
	configuration := cluster.Spec.Backup.BarmanObjectStore
	var storageAccountSecret corev1.Secret

	// Get storage account name
	if configuration.AzureCredentials.StorageAccount != nil {
		storageAccountName := configuration.AzureCredentials.StorageAccount.Name
		storageAccountKey := configuration.AzureCredentials.StorageAccount.Key
		err := c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: storageAccountName}, &storageAccountSecret)
		if err != nil {
			return nil, fmt.Errorf("while getting access key ID secret: %w", err)
		}

		storageAccount, ok := storageAccountSecret.Data[storageAccountKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside storage account name secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_ACCOUNT=%s", storageAccount))
	}

	// Get the storage key
	if configuration.AzureCredentials.StorageKey != nil {
		var storageKeySecret corev1.Secret
		storageKeyName := configuration.AzureCredentials.StorageKey.Name
		storageKeyKey := configuration.AzureCredentials.StorageKey.Key

		err := c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: storageKeyName}, &storageKeySecret)
		if err != nil {
			return nil, fmt.Errorf("while getting access key ID secret: %w", err)
		}

		storageKey, ok := storageKeySecret.Data[storageKeyKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside storage key secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_KEY=%s", storageKey))
	}

	// Get the SAS token
	if configuration.AzureCredentials.StorageSasToken != nil {
		var storageSasTokenSecret corev1.Secret
		storageSasTokenName := configuration.AzureCredentials.StorageSasToken.Name
		storageSasTokenKey := configuration.AzureCredentials.StorageSasToken.Key

		err := c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: storageSasTokenName}, &storageSasTokenSecret)
		if err != nil {
			return nil, fmt.Errorf("while getting storage SAS token secret: %w", err)
		}

		storageKey, ok := storageSasTokenSecret.Data[storageSasTokenKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside storage SAS token secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_SAS_TOKEN=%s", storageKey))
	}

	if configuration.AzureCredentials.ConnectionString != nil {
		var connectionStringSecret corev1.Secret
		connectionStringName := configuration.AzureCredentials.ConnectionString.Name
		connectionStringKey := configuration.AzureCredentials.ConnectionString.Key

		err := c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: connectionStringName}, &connectionStringSecret)
		if err != nil {
			return nil, fmt.Errorf("while getting storage SAS token secret: %w", err)
		}

		storageKey, ok := connectionStringSecret.Data[connectionStringKey]
		if !ok {
			return nil, fmt.Errorf("missing key inside connection string secret")
		}
		env = append(env, fmt.Sprintf("AZURE_STORAGE_CONNECTION_STRING=%s", storageKey))
	}

	return env, nil
}

// getBackupList returns the backup list a
func (b *BackupCommand) getBackupList() (*BackupListResult, error) {
	barmanConfiguration := b.Cluster.Spec.Backup.BarmanObjectStore
	backupStatus := b.Backup.GetStatus()

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	options := []string{"--format", "json"}
	if barmanConfiguration.EndpointURL != "" {
		options = append(options, "--endpoint-url", barmanConfiguration.EndpointURL)
	}
	if barmanConfiguration.Data != nil && barmanConfiguration.Data.Encryption != "" {
		options = append(options, "-e", string(barmanConfiguration.Data.Encryption))
	}
	if barmanConfiguration.S3Credentials != nil {
		options = append(
			options,
			"--cloud-provider",
			"aws-s3")
	}
	if barmanConfiguration.AzureCredentials != nil {
		options = append(
			options,
			"--cloud-provider",
			"azure-blob-storage")
	}
	options = append(options, barmanConfiguration.DestinationPath, backupStatus.ServerName)

	cmd := exec.Command("barman-cloud-backup-list", options...) // #nosec G204
	cmd.Env = b.Env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err := cmd.Run()
	if err != nil {
		b.Log.Error(err,
			"Can't extract backup id using barman-cloud-backup-list",
			"options", options,
			"stdout", stdoutBuffer.String(),
			"stderr", stderrBuffer.String())
		return nil, err
	}

	backupList, err := parseBarmanCloudBackupList(stdoutBuffer.String())
	if err != nil {
		b.Log.Error(err, "Can't parse barman-cloud-backup-list output")
		return nil, err
	}

	return backupList, nil
}

// parseBarmanCloudBackupList parses the output of barman-cloud-backup-list
func parseBarmanCloudBackupList(output string) (*BackupListResult, error) {
	result := &BackupListResult{}
	err := json.Unmarshal([]byte(output), result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
