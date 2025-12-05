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

package postgres

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"time"

	barmanBackup "github.com/cloudnative-pg/barman-cloud/pkg/backup"
	barmanCatalog "github.com/cloudnative-pg/barman-cloud/pkg/catalog"
	barmanCommand "github.com/cloudnative-pg/barman-cloud/pkg/command"
	barmanCredentials "github.com/cloudnative-pg/barman-cloud/pkg/credentials"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"

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
	Client       client.Client
	Recorder     record.EventRecorder
	Env          []string
	Log          log.Logger
	Instance     *Instance
	barmanBackup *barmanBackup.Command
}

// NewBarmanBackupCommand initializes a BackupCommand object, taking a physical
// backup using Barman Cloud
func NewBarmanBackupCommand(
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	client client.Client,
	recorder record.EventRecorder,
	instance *Instance,
	log log.Logger,
) (*BackupCommand, error) {
	return &BackupCommand{
		Cluster:      cluster,
		Backup:       backup,
		Client:       client,
		Recorder:     recorder,
		Env:          os.Environ(),
		Instance:     instance,
		Log:          log,
		barmanBackup: barmanBackup.NewBackupCommand(cluster.Spec.Backup.BarmanObjectStore),
	}, nil
}

// Start initiates a backup for this instance using
// barman-cloud-backup
func (b *BackupCommand) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	b.setupBackupStatus()

	err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup)
	if err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	if err := ensureWalArchiveIsWorking(b.Instance); err != nil {
		contextLogger.Warning("WAL archiving is not working", "err", err)
		b.Backup.GetStatus().Phase = apiv1.BackupPhaseWalArchivingFailing
		return PatchBackupStatusAndRetry(ctx, b.Client, b.Backup)
	}

	if b.Backup.GetStatus().Phase != apiv1.BackupPhaseRunning {
		b.Backup.GetStatus().Phase = apiv1.BackupPhaseRunning
		err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup)
		if err != nil {
			contextLogger.Error(err, "can't set backup as WAL archiving failing")
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

func (b *BackupCommand) retryWithRefreshedCluster(
	ctx context.Context,
	cb func() error,
) error {
	return resources.RetryWithRefreshedResource(ctx, b.Client, b.Cluster, cb)
}

// run executes the barman-cloud-backup command and updates the status
// This method will take long time and is supposed to run inside a dedicated
// goroutine.
func (b *BackupCommand) run(ctx context.Context) {
	ctx = log.IntoContext(
		ctx,
		log.FromContext(ctx).
			WithValues(
				"backupName", b.Backup.Name,
				"backupNamespace", b.Backup.Namespace,
			),
	)

	if err := b.takeBackup(ctx); err != nil {
		// record the failure
		b.Log.Error(err, "Backup failed")
		b.Recorder.Event(b.Backup, "Normal", "Failed", "Backup failed")

		_ = status.FlagBackupAsFailed(ctx, b.Client, b.Backup, b.Cluster, err)
	}

	b.backupMaintenance(ctx)
}

func (b *BackupCommand) takeBackup(ctx context.Context) error {
	backupStatus := b.Backup.GetStatus()

	b.Recorder.Event(b.Backup, "Normal", "Starting", "Backup started")

	// Update backup status in cluster conditions on startup
	if err := b.retryWithRefreshedCluster(ctx, func() error {
		return status.PatchConditionsWithOptimisticLock(ctx, b.Client, b.Cluster, apiv1.BackupStartingCondition)
	}); err != nil {
		b.Log.Error(err, "Error changing backup condition (backup started)")
		// We do not terminate here because we could still have a good backup
		// even if we are unable to communicate with the Kubernetes API server
	}

	if err := fileutils.EnsureDirectoryExists(postgres.BackupTemporaryDirectory); err != nil {
		b.Log.Error(err, "Cannot create backup temporary directory", "err", err)
		return err
	}

	err := b.barmanBackup.Take(
		ctx,
		b.Backup.Status.BackupName,
		backupStatus.ServerName,
		b.Env,
		postgres.BackupTemporaryDirectory,
	)
	if err != nil {
		b.Log.Error(err, "Error while taking barman backup", "err", err)
		return err
	}

	b.Log.Info("Backup completed")
	b.Recorder.Event(b.Backup, "Normal", "Completed", "Backup completed")

	// Set the status to completed
	b.Backup.Status.SetAsCompleted()

	barmanBackup, err := b.barmanBackup.GetExecutedBackupInfo(
		ctx, b.Backup.Status.BackupName, backupStatus.ServerName, b.Env)
	if err != nil {
		return err
	}

	b.Log.Debug("extracted barman backup", "backup", barmanBackup)
	assignBarmanBackupToBackup(b.Backup, barmanBackup)

	if err := PatchBackupStatusAndRetry(ctx, b.Client, b.Backup); err != nil {
		b.Log.Error(err, "Can't set backup status as completed")
	}

	// Update backup status in cluster conditions on backup completion
	if err := b.retryWithRefreshedCluster(ctx, func() error {
		return status.PatchConditionsWithOptimisticLock(ctx, b.Client, b.Cluster, apiv1.BackupSucceededCondition)
	}); err != nil {
		b.Log.Error(err, "Can't update the cluster with the completed backup data")
	}

	return nil
}

func (b *BackupCommand) backupMaintenance(ctx context.Context) {
	// Delete backups per policy
	if b.Cluster.Spec.Backup.RetentionPolicy != "" {
		// TODO: refactor retention policy and move it in the Barman library
		b.Log.Info("Applying backup retention policy",
			"retentionPolicy", b.Cluster.Spec.Backup.RetentionPolicy)
		if err := barmanCommand.DeleteBackupsByPolicy(
			ctx,
			b.Cluster.Spec.Backup.BarmanObjectStore,
			b.Backup.Status.ServerName,
			b.Env,
			b.Cluster.Spec.Backup.RetentionPolicy,
		); err != nil {
			// Proper logging already happened inside DeleteBackupsByPolicy
			b.Recorder.Event(b.Cluster, "Warning", "RetentionPolicyFailed", "Retention policy failed")
			// We do not want to return here, we must go on to set the fist recoverability point
		}
	}

	data, err := b.getBackupData(ctx)
	if err != nil {
		// Proper logging already happened inside GetBackupList
		return
	}

	if err := deleteBackupsNotInCatalog(ctx, b.Client, b.Cluster, data.GetBackupIDs()); err != nil {
		b.Log.Error(err, "while deleting Backups not present in the catalog")
	}

	if err := b.retryWithRefreshedCluster(ctx, func() error {
		origCluster := b.Cluster.DeepCopy()

		// Set the first recoverability point and the last successful backup
		b.Cluster.UpdateBackupTimes(
			apiv1.BackupMethod(data.GetBackupMethod()),
			data.GetFirstRecoverabilityPoint(),
			data.GetLastSuccessfulBackupTime(),
		)

		if equality.Semantic.DeepEqual(origCluster.Status, b.Cluster.Status) {
			return nil
		}
		return b.Client.Status().Patch(ctx, b.Cluster, client.MergeFrom(origCluster))
	}); err != nil {
		b.Log.Error(err, "while setting the firstRecoverabilityPoint and latestSuccessfulBackup")
	}
}

// PatchBackupStatusAndRetry updates a certain backup's status in the k8s database,
// retries when error occurs
// TODO: this method does not belong here, it should be moved to api/v1/backup_types.go
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

	backupStatus.BackupName = fmt.Sprintf("backup-%v", pgTime.ToCompactISO8601(time.Now()))
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

func assignBarmanBackupToBackup(backup *apiv1.Backup, barmanBackup *barmanCatalog.BarmanBackup) {
	backupStatus := backup.GetStatus()

	backupStatus.BackupName = barmanBackup.BackupName
	backupStatus.BackupID = barmanBackup.ID
	backupStatus.StartedAt = &metav1.Time{Time: barmanBackup.BeginTime}
	backupStatus.StoppedAt = &metav1.Time{Time: barmanBackup.EndTime}
	backupStatus.BeginWal = barmanBackup.BeginWal
	backupStatus.EndWal = barmanBackup.EndWal
	backupStatus.BeginLSN = barmanBackup.BeginLSN
	backupStatus.EndLSN = barmanBackup.EndLSN
}

// deleteBackupsNotInCatalog deletes all Backup objects pointing to the given cluster that are not
// present in the backup anymore
func deleteBackupsNotInCatalog(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	backupIDs []string,
) error {
	// We had two options:
	//
	// A. quicker
	// get policy checker function
	// get all backups in the namespace for this cluster
	// check with policy checker function if backup should be deleted, then delete it if true
	//
	// B. more precise
	// get the catalog (GetBackupList)
	// get all backups in the namespace for this cluster
	// go through all backups and delete them if not in the catalog
	//
	// 1: all backups in the bucket should be also in the cluster
	// 2: all backups in the cluster should be in the bucket
	//
	// A can violate 1 and 2
	// A + B can still violate 2
	// B satisfies 1 and 2

	// We chose to go with B

	backups := apiv1.BackupList{}
	err := cli.List(ctx, &backups, client.InNamespace(cluster.GetNamespace()))
	if err != nil {
		return fmt.Errorf("while getting backups: %w", err)
	}

	var errors []error
	for id, backup := range backups.Items {
		if backup.Spec.Cluster.Name != cluster.GetName() ||
			backup.Status.Phase != apiv1.BackupPhaseCompleted ||
			!useSameBackupLocation(&backup.Status, cluster) {
			continue
		}

		// here we could add further checks, e.g. if the backup is not found but would still
		// be in the retention policy we could either not delete it or update it is status
		if !slices.Contains(backupIDs, backup.Status.BackupID) {
			err := cli.Delete(ctx, &backups.Items[id])
			if err != nil {
				errors = append(errors, fmt.Errorf(
					"while deleting backup %s/%s: %w",
					backup.Namespace,
					backup.Name,
					err,
				))
			}
		}
	}

	if errors != nil {
		return fmt.Errorf("got errors while deleting Backups not in the cluster: %v", errors)
	}
	return nil
}

// useSameBackupLocation checks whether the given backup was taken using the same configuration as provided
func useSameBackupLocation(backup *apiv1.BackupStatus, cluster *apiv1.Cluster) bool {
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		return false
	}
	configuration := cluster.Spec.Backup.BarmanObjectStore
	return backup.EndpointURL == configuration.EndpointURL &&
		backup.DestinationPath == configuration.DestinationPath &&
		(backup.ServerName == configuration.ServerName ||
			// if not specified we use the cluster name as server name
			(configuration.ServerName == "" && backup.ServerName == cluster.Name)) &&
		reflect.DeepEqual(backup.BarmanCredentials, configuration.BarmanCredentials)
}

type backupDataGetter interface {
	GetFirstRecoverabilityPoint() *time.Time
	GetLastSuccessfulBackupTime() *time.Time
	GetBackupIDs() []string
	GetBackupMethod() string
}

func (b *BackupCommand) getBackupData(ctx context.Context) (backupDataGetter, error) {
	// TODO: here we can inject any plugin logic and skip the default barman execution

	// Extracting the latest backup using barman-cloud-backup-list
	return barmanCommand.GetBackupList(
		ctx,
		b.Cluster.Spec.Backup.BarmanObjectStore,
		b.Backup.Status.ServerName,
		b.Env,
	)
}
