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
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// BackupListResult represent the result of a
// barman-cloud-backup-list invocation
type BackupListResult struct {
	// The list of backups
	List []BarmanBackup `json:"backups_list"`
}

// GetLatestBackupID gets the latest backup ID
// between the ones available in the list
func (backupList *BackupListResult) GetLatestBackupID() string {
	id := ""
	for _, backup := range backupList.List {
		if backup.ID > id {
			id = backup.ID
		}
	}
	return id
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

	// The systemID of the cluster
	SystemID string `json:"systemid"`

	// The ID of the backup
	ID string `json:"backup_id"`
}

// Backup start a backup for this instance using
// barman-cloud-backup
func (instance *Instance) Backup(
	ctx context.Context,
	client client.Client,
	recorder record.EventRecorder,
	cluster *apiv1.Cluster,
	backupObject runtime.Object,
	log logr.Logger,
) error {
	configuration := cluster.Spec.Backup.BarmanObjectStore

	serverName := instance.ClusterName
	if len(configuration.ServerName) != 0 {
		serverName = configuration.ServerName
	}

	options := instance.getBarmanCloudBackupOptions(configuration, serverName)

	// Mark the backup as running
	backup := backupObject.(apiv1.BackupCommon)
	if backup == nil {
		return fmt.Errorf("backup object not recognized")
	}

	backup.GetStatus().S3Credentials = configuration.S3Credentials
	backup.GetStatus().EndpointURL = configuration.EndpointURL
	backup.GetStatus().DestinationPath = configuration.DestinationPath
	backup.GetStatus().ServerName = instance.ClusterName
	if configuration.Data != nil {
		backup.GetStatus().Encryption = string(configuration.Data.Encryption)
	}
	if len(configuration.ServerName) != 0 {
		backup.GetStatus().ServerName = configuration.ServerName
	}
	backup.GetStatus().Phase = apiv1.BackupPhaseRunning
	backup.GetStatus().StartedAt = &metav1.Time{
		Time: time.Now(),
	}

	if err := utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject()); err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	// Run the actual backup process
	go func() {
		log.Info("Backup started",
			"options",
			options)

		if err := SetAWSCredentials(ctx, client, cluster); err != nil {
			log.Error(err, "Cannot recover AWS credentials", "err", err)
			return
		}

		recorder.Event(backupObject, "Normal", "Starting", "Backup started")

		if err := fileutils.EnsureDirectoryExist(postgres.BackupTemporaryDirectory); err != nil {
			log.Error(err, "Cannot create backup temporary directory", "err", err)
			return
		}

		cmd := exec.Command("barman-cloud-backup", options...) // #nosec G204
		var stdoutBuffer bytes.Buffer
		var stderrBuffer bytes.Buffer
		cmd.Stdout = &stdoutBuffer
		cmd.Stderr = &stderrBuffer
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "TMPDIR="+postgres.BackupTemporaryDirectory)
		err := cmd.Run()

		log.Info("Backup completed", "err", err)

		if err != nil {
			backup.GetStatus().SetAsFailed(stdoutBuffer.String(), stderrBuffer.String(), err)
			recorder.Event(backupObject, "Normal", "Failed", "Backup failed")
		} else {
			backup.GetStatus().SetAsCompleted(stdoutBuffer.String(), stderrBuffer.String())
			recorder.Event(backupObject, "Normal", "Completed", "Backup completed")
		}
		backup.GetStatus().StoppedAt = &metav1.Time{
			Time: time.Now(),
		}

		if err := utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject()); err != nil {
			log.Error(err,
				"Can't mark backup as done",
				"stdout", stdoutBuffer.String(),
				"stderr", stderrBuffer.String())
		}

		// Extracting backup ID using barman-cloud-backup-list
		options := []string{"--format", "json"}
		if configuration.EndpointURL != "" {
			options = append(options, "--endpoint-url", configuration.EndpointURL)
		}
		if configuration.Data != nil && configuration.Data.Encryption != "" {
			options = append(options, "-e", string(configuration.Data.Encryption))
		}
		options = append(options, configuration.DestinationPath, serverName)

		cmd = exec.Command("barman-cloud-backup-list", options...) // #nosec G204
		stderrBuffer.Reset()
		stdoutBuffer.Reset()
		cmd.Stdout = &stdoutBuffer
		cmd.Stderr = &stderrBuffer
		err = cmd.Run()

		if err != nil {
			log.Error(err,
				"Can't extract backup id using barman-cloud-backup-list",
				"options", options,
				"stdout", stdoutBuffer.String(),
				"stderr", stderrBuffer.String())
			return
		}

		backupList, err := parseBarmanCloudBackupList(stdoutBuffer.String())
		if err != nil {
			log.Error(err,
				"Error parsing barman-cloud-backup-list output",
				"stdout", stdoutBuffer.String(),
				"stderr", stderrBuffer.String())
			return
		}

		backup.GetStatus().BackupID = backupList.GetLatestBackupID()
		if err := utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject()); err != nil {
			log.Error(err,
				"Can't mark backup with Barman ID",
				"backupID", backup.GetStatus().BackupID)
		}
	}()

	return nil
}

// getBarmanCloudBackupOptions extract the list of command line options to be used with
// barman-cloud-backup
func (instance *Instance) getBarmanCloudBackupOptions(
	configuration *apiv1.BarmanObjectStoreConfiguration, serverName string) []string {
	options := []string{
		"-U", "postgres",
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
				"-e",
				string(configuration.Data.Encryption))
		}
	}
	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}
	options = append(
		options,
		configuration.DestinationPath,
		serverName)
	return options
}

// parse the output of barman-cloud-backup-list
func parseBarmanCloudBackupList(output string) (*BackupListResult, error) {
	var result BackupListResult
	err := json.Unmarshal([]byte(output), &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// SetAWSCredentials set the AWS environment variables given the configuration
// inside the cluster
func SetAWSCredentials(ctx context.Context, c client.Client, cluster *apiv1.Cluster) error {
	configuration := cluster.Spec.Backup.BarmanObjectStore
	var accessKeyIDSecret corev1.Secret
	var secretAccessKeySecret corev1.Secret

	// Get access key ID
	secretName := configuration.S3Credentials.AccessKeyIDReference.Name
	secretKey := configuration.S3Credentials.AccessKeyIDReference.Key
	err := c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &accessKeyIDSecret)
	if err != nil {
		return fmt.Errorf("while getting access key ID secret: %w", err)
	}

	accessKeyID, ok := accessKeyIDSecret.Data[secretKey]
	if !ok {
		return fmt.Errorf("missing key inside access key ID secret")
	}

	// Get secret access key
	secretName = configuration.S3Credentials.SecretAccessKeyReference.Name
	secretKey = configuration.S3Credentials.SecretAccessKeyReference.Key
	err = c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &secretAccessKeySecret)
	if err != nil {
		return fmt.Errorf("while getting secret access key secret: %w", err)
	}

	secretAccessKey, ok := secretAccessKeySecret.Data[secretKey]
	if !ok {
		return fmt.Errorf("missing key inside secret access key secret")
	}

	// Setting environment variables
	err = os.Setenv("AWS_ACCESS_KEY_ID", string(accessKeyID))
	if err != nil {
		return err
	}
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", string(secretAccessKey))
	if err != nil {
		return err
	}

	return nil
}
