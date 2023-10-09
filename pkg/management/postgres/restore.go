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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walarchive"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/archiver"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	barmanCredentials "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/credentials"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/restorer"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/catalog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var (
	// ErrInstanceInRecovery is raised while PostgreSQL is still in recovery mode
	ErrInstanceInRecovery = fmt.Errorf("instance in recovery")

	// RetryUntilRecoveryDone is the default retry configuration that is used
	// to wait for a restored cluster to promote itself
	RetryUntilRecoveryDone = wait.Backoff{
		Duration: 5 * time.Second,
		// Steps is declared as an "int", so we are capping
		// to int32 to support ARM-based 32 bit architectures
		Steps: math.MaxInt32,
	}

	pgControldataSettingsToParamsMap = map[string]string{
		"max_connections setting":      "max_connections",
		"max_wal_senders setting":      "max_wal_senders",
		"max_worker_processes setting": "max_worker_processes",
		"max_prepared_xacts setting":   "max_prepared_transactions",
		"max_locks_per_xact setting":   "max_locks_per_transaction",
	}
)

// RestoreSnapshot restores a PostgreSQL cluster from a volumeSnapshot
func (info InitInfo) RestoreSnapshot(ctx context.Context, cli client.Client, immediate bool) error {
	contextLogger := log.FromContext(ctx)

	cluster, err := info.loadCluster(ctx, cli)
	if err != nil {
		return err
	}

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	contextLogger.Info("Cleaning up PGDATA from stale files")
	if err := fileutils.RemoveRestoreExcludedFiles(ctx, info.PgData); err != nil {
		return fmt.Errorf("error while cleaning up the recovered PGDATA: %w", err)
	}

	if immediate || cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil ||
		cluster.Spec.Bootstrap.Recovery.Source == "" {
		// We are recovering from an existing PVC snapshot, we
		// don't need to invoke the recovery job
		return nil
	}

	contextLogger.Info("Recovering from volume snapshot",
		"sourceName", cluster.Spec.Bootstrap.Recovery.Source)

	backup, env, err := info.createBackupObjectForSnapshotRestore(ctx, cli, cluster)
	if err != nil {
		return err
	}

	if _, err := info.restoreCustomWalDir(ctx); err != nil {
		return err
	}

	if err := info.WriteInitialPostgresqlConf(cluster); err != nil {
		return err
	}

	if cluster.IsReplica() {
		server, ok := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)
		if !ok {
			return fmt.Errorf("missing external cluster: %v", cluster.Spec.ReplicaCluster.Source)
		}

		connectionString, _, err := external.ConfigureConnectionToServer(
			ctx, cli, info.Namespace, &server)
		if err != nil {
			return err
		}

		// TODO: Using a replication slot on replica cluster is not supported (yet?)
		_, err = UpdateReplicaConfiguration(info.PgData, connectionString, "")
		return err
	}

	if err := info.WriteRestoreHbaConf(); err != nil {
		return err
	}

	if err := info.writeRestoreWalConfig(backup, cluster); err != nil {
		return err
	}

	return info.ConfigureInstanceAfterRestore(ctx, cluster, env)
}

// createBackupObjectForSnapshotRestore creates a fake Backup object that can be used during the
// snapshot restore process
func (info InitInfo) createBackupObjectForSnapshotRestore(
	ctx context.Context,
	typedClient client.Client,
	cluster *apiv1.Cluster,
) (*apiv1.Backup, []string, error) {
	sourceName := cluster.Spec.Bootstrap.Recovery.Source

	if sourceName == "" {
		return nil, nil, fmt.Errorf("recovery source not specified")
	}

	log.Info("Recovering from external cluster", "sourceName", sourceName)

	server, found := cluster.ExternalCluster(sourceName)
	if !found {
		return nil, nil, fmt.Errorf("missing external cluster: %v", sourceName)
	}
	serverName := server.GetServerName()

	env, err := barmanCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		typedClient,
		cluster.Namespace,
		server.BarmanObjectStore,
		os.Environ())
	if err != nil {
		return nil, nil, err
	}

	return &apiv1.Backup{
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: serverName,
			},
		},
		Status: apiv1.BackupStatus{
			BarmanCredentials: server.BarmanObjectStore.BarmanCredentials,
			EndpointCA:        server.BarmanObjectStore.EndpointCA,
			EndpointURL:       server.BarmanObjectStore.EndpointURL,
			DestinationPath:   server.BarmanObjectStore.DestinationPath,
			ServerName:        serverName,
			Phase:             apiv1.BackupPhaseCompleted,
		},
	}, env, nil
}

// Restore restores a PostgreSQL cluster from a backup into the object storage
func (info InitInfo) Restore(ctx context.Context) error {
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return err
	}

	cluster, err := info.loadCluster(ctx, typedClient)
	if err != nil {
		return err
	}

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	if cluster.ShouldRecoveryCreateApplicationDatabase() {
		info.ApplicationUser = cluster.GetApplicationDatabaseOwner()
		info.ApplicationDatabase = cluster.GetApplicationDatabaseName()
	}

	// Before starting the restore we check if the archive destination is safe to use
	// otherwise, we stop creating the cluster
	err = info.checkBackupDestination(ctx, typedClient, cluster)
	if err != nil {
		return err
	}

	// If we need to download data from a backup, we do it
	backup, env, err := info.loadBackup(ctx, typedClient, cluster)
	if err != nil {
		return err
	}

	if err := info.ensureArchiveContainsLastCheckpointRedoWAL(ctx, cluster, env, backup); err != nil {
		return err
	}

	if err := info.restoreDataDir(backup, env); err != nil {
		return err
	}

	if _, err := info.restoreCustomWalDir(ctx); err != nil {
		return err
	}

	if err := info.WriteInitialPostgresqlConf(cluster); err != nil {
		return err
	}

	if cluster.IsReplica() {
		server, ok := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)
		if !ok {
			return fmt.Errorf("missing external cluster: %v", cluster.Spec.ReplicaCluster.Source)
		}

		connectionString, _, err := external.ConfigureConnectionToServer(
			ctx, typedClient, info.Namespace, &server)
		if err != nil {
			return err
		}

		// TODO: Using a replication slot on replica cluster is not supported (yet?)
		_, err = UpdateReplicaConfiguration(info.PgData, connectionString, "")
		return err
	}

	if err := info.WriteRestoreHbaConf(); err != nil {
		return err
	}

	if err := info.writeRestoreWalConfig(backup, cluster); err != nil {
		return err
	}

	return info.ConfigureInstanceAfterRestore(ctx, cluster, env)
}

func (info InitInfo) ensureArchiveContainsLastCheckpointRedoWAL(
	ctx context.Context,
	cluster *apiv1.Cluster,
	env []string,
	backup *apiv1.Backup,
) error {
	// it's the full path of the file that will temporarily contain the LastCheckpointRedoWAL
	const testWALPath = postgresSpec.RecoveryTemporaryDirectory + "/test.wal"
	contextLogger := log.FromContext(ctx)

	defer func() {
		if err := fileutils.RemoveFile(testWALPath); err != nil {
			contextLogger.Error(err, "while deleting the temporary wal file: %w")
		}
	}()

	if err := fileutils.EnsureParentDirectoryExist(testWALPath); err != nil {
		return err
	}

	rest, err := restorer.New(ctx, cluster, env, walarchive.SpoolDirectory)
	if err != nil {
		return err
	}

	opts, err := barman.CloudWalRestoreOptions(&apiv1.BarmanObjectStoreConfiguration{
		BarmanCredentials: backup.Status.BarmanCredentials,
		EndpointCA:        backup.Status.EndpointCA,
		EndpointURL:       backup.Status.EndpointURL,
		DestinationPath:   backup.Status.DestinationPath,
		ServerName:        backup.Status.ServerName,
	}, cluster.Name)
	if err != nil {
		return err
	}

	if err := rest.Restore(backup.Status.BeginWal, testWALPath, opts); err != nil {
		return fmt.Errorf("encountered an error while checking the presence of first needed WAL in the archive: %w", err)
	}

	return nil
}

// restoreCustomWalDir moves the current pg_wal data to the specified custom wal dir and applies the symlink
// returns indicating if any changes were made and any error encountered in the process
func (info InitInfo) restoreCustomWalDir(ctx context.Context) (bool, error) {
	if info.PgWal == "" {
		return false, nil
	}

	contextLogger := log.FromContext(ctx)
	pgDataWal := path.Join(info.PgData, "pg_wal")

	// if the link is already present we have nothing to do.
	if linkInfo, _ := os.Readlink(pgDataWal); linkInfo == info.PgWal {
		contextLogger.Info("symlink to the WAL volume already present, skipping the custom wal dir restore")
		return false, nil
	}

	if err := fileutils.EnsureDirectoryExists(info.PgWal); err != nil {
		return false, err
	}

	contextLogger.Info("restoring WAL volume symlink and transferring data")
	if err := fileutils.EnsureDirectoryExists(pgDataWal); err != nil {
		return false, err
	}

	if err := fileutils.MoveDirectoryContent(pgDataWal, info.PgWal); err != nil {
		return false, err
	}

	if err := fileutils.RemoveFile(pgDataWal); err != nil {
		return false, err
	}

	return true, os.Symlink(info.PgWal, pgDataWal)
}

// restoreDataDir restores PGDATA from an existing backup
func (info InitInfo) restoreDataDir(backup *apiv1.Backup, env []string) error {
	var options []string

	if backup.Status.EndpointURL != "" {
		options = append(options, "--endpoint-url", backup.Status.EndpointURL)
	}
	options = append(options, backup.Status.DestinationPath)
	options = append(options, backup.Status.ServerName)
	options = append(options, backup.Status.BackupID)

	options, err := barman.AppendCloudProviderOptionsFromBackup(options, backup)
	if err != nil {
		return err
	}

	options = append(options, info.PgData)

	log.Info("Starting barman-cloud-restore",
		"options", options)

	cmd := exec.Command(barmanCapabilities.BarmanCloudRestore, options...) // #nosec G204
	cmd.Env = env
	err = execlog.RunStreaming(cmd, barmanCapabilities.BarmanCloudRestore)
	if err != nil {
		log.Error(err, "Can't restore backup")
		return err
	}
	log.Info("Restore completed")
	return nil
}

// loadCluster loads the cluster definition from the API server
func (info InitInfo) loadCluster(ctx context.Context, typedClient client.Client) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := typedClient.Get(ctx, client.ObjectKey{Namespace: info.Namespace, Name: info.ClusterName}, &cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}

// loadBackup loads the backup manifest from the API server of from the object store.
// It also gets the environment variables that are needed to recover the cluster
func (info InitInfo) loadBackup(
	ctx context.Context,
	typedClient client.Client,
	cluster *apiv1.Cluster,
) (*apiv1.Backup, []string, error) {
	// Recovery given an existing backup
	if cluster.Spec.Bootstrap.Recovery.Backup != nil {
		return info.loadBackupFromReference(ctx, typedClient, cluster)
	}

	return info.loadBackupObjectFromExternalCluster(ctx, typedClient, cluster)
}

// loadBackupObjectFromExternalCluster generates an in-memory Backup structure given a reference to
// an external cluster, loading the required information from the object store
func (info InitInfo) loadBackupObjectFromExternalCluster(
	ctx context.Context,
	typedClient client.Client,
	cluster *apiv1.Cluster,
) (*apiv1.Backup, []string, error) {
	sourceName := cluster.Spec.Bootstrap.Recovery.Source

	if sourceName == "" {
		return nil, nil, fmt.Errorf("recovery source not specified")
	}

	log.Info("Recovering from external cluster", "sourceName", sourceName)

	server, found := cluster.ExternalCluster(sourceName)
	if !found {
		return nil, nil, fmt.Errorf("missing external cluster: %v", sourceName)
	}
	serverName := server.GetServerName()

	env, err := barmanCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		typedClient,
		cluster.Namespace,
		server.BarmanObjectStore,
		os.Environ())
	if err != nil {
		return nil, nil, err
	}

	backupCatalog, err := barman.GetBackupList(ctx, server.BarmanObjectStore, serverName, env)
	if err != nil {
		return nil, nil, err
	}

	// We are now choosing the right backup to restore
	var targetBackup *catalog.BarmanBackup
	if cluster.Spec.Bootstrap.Recovery != nil &&
		cluster.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
		targetBackup, err = backupCatalog.FindBackupInfo(cluster.Spec.Bootstrap.Recovery.RecoveryTarget)
		if err != nil {
			return nil, nil, err
		}
	} else {
		targetBackup = backupCatalog.LatestBackupInfo()
	}
	if targetBackup == nil {
		return nil, nil, fmt.Errorf("no target backup found")
	}

	log.Info("Target backup found", "backup", targetBackup)

	return &apiv1.Backup{
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: serverName,
			},
		},
		Status: apiv1.BackupStatus{
			BarmanCredentials: server.BarmanObjectStore.BarmanCredentials,
			EndpointCA:        server.BarmanObjectStore.EndpointCA,
			EndpointURL:       server.BarmanObjectStore.EndpointURL,
			DestinationPath:   server.BarmanObjectStore.DestinationPath,
			ServerName:        serverName,
			BackupID:          targetBackup.ID,
			Phase:             apiv1.BackupPhaseCompleted,
			StartedAt:         &metav1.Time{Time: targetBackup.BeginTime},
			StoppedAt:         &metav1.Time{Time: targetBackup.EndTime},
			BeginWal:          targetBackup.BeginWal,
			EndWal:            targetBackup.EndWal,
			BeginLSN:          targetBackup.BeginLSN,
			EndLSN:            targetBackup.EndLSN,
			Error:             targetBackup.Error,
			CommandOutput:     "",
			CommandError:      "",
		},
	}, env, nil
}

// loadBackupFromReference loads a backup object and the required credentials given the backup object resource
func (info InitInfo) loadBackupFromReference(
	ctx context.Context,
	typedClient client.Client,
	cluster *apiv1.Cluster,
) (*apiv1.Backup, []string, error) {
	var backup apiv1.Backup
	err := typedClient.Get(
		ctx,
		client.ObjectKey{Namespace: info.Namespace, Name: cluster.Spec.Bootstrap.Recovery.Backup.Name},
		&backup)
	if err != nil {
		return nil, nil, err
	}

	env, err := barmanCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		typedClient,
		cluster.Namespace,
		&apiv1.BarmanObjectStoreConfiguration{
			BarmanCredentials: backup.Status.BarmanCredentials,
			EndpointCA:        backup.Status.EndpointCA,
			EndpointURL:       backup.Status.EndpointURL,
			DestinationPath:   backup.Status.DestinationPath,
			ServerName:        backup.Status.ServerName,
		},
		os.Environ())
	if err != nil {
		return nil, nil, err
	}

	log.Info("Recovering existing backup", "backup", backup)
	return &backup, env, nil
}

// writeRestoreWalConfig writes a `custom.conf` allowing PostgreSQL
// to complete the WAL recovery from the object storage and then start
// as a new primary
func (info InitInfo) writeRestoreWalConfig(backup *apiv1.Backup, cluster *apiv1.Cluster) error {
	var err error

	cmd := []string{barmanCapabilities.BarmanCloudWalRestore}
	if backup.Status.EndpointURL != "" {
		cmd = append(cmd, "--endpoint-url", backup.Status.EndpointURL)
	}
	cmd = append(cmd, backup.Status.DestinationPath)
	cmd = append(cmd, backup.Status.ServerName)

	cmd, err = barman.AppendCloudProviderOptionsFromBackup(cmd, backup)
	if err != nil {
		return err
	}

	cmd = append(cmd, "%f", "%p")

	recoveryFileContents := fmt.Sprintf(
		"recovery_target_action = promote\n"+
			"restore_command = '%s'\n"+
			"%s",
		strings.Join(cmd, " "),
		cluster.Spec.Bootstrap.Recovery.RecoveryTarget.BuildPostgresOptions())

	return info.writeRecoveryConfiguration(recoveryFileContents)
}

func (info InitInfo) writeRecoveryConfiguration(recoveryFileContents string) error {
	// Ensure restore_command is used to correctly recover WALs
	// from the object storage
	major, err := postgresutils.GetMajorVersion(info.PgData)
	if err != nil {
		return fmt.Errorf("cannot detect major version: %w", err)
	}

	log.Info("Generated recovery configuration", "configuration", recoveryFileContents)
	// Disable archiving
	err = fileutils.AppendStringToFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
		"archive_command = 'cd .'\n")
	if err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	enforcedParams, err := GetEnforcedParametersThroughPgControldata(info.PgData)
	if err != nil {
		return err
	}
	if enforcedParams != nil {
		changed, err := configfile.UpdatePostgresConfigurationFile(
			path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
			enforcedParams,
		)
		if changed {
			log.Info("enforcing parameters found in pg_controldata", "parameters", enforcedParams)
		}
		if err != nil {
			return fmt.Errorf("cannot write recovery config for enforced parameters: %w", err)
		}
	}

	if major >= 12 {
		// Append restore_command to the end of the
		// custom configs file
		err = fileutils.AppendStringToFile(
			path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
			recoveryFileContents)
		if err != nil {
			return fmt.Errorf("cannot write recovery config: %w", err)
		}

		err = os.WriteFile(
			path.Join(info.PgData, "postgresql.auto.conf"),
			[]byte(""),
			0o600)
		if err != nil {
			return fmt.Errorf("cannot erase auto config: %w", err)
		}

		// Create recovery signal file
		return os.WriteFile(
			path.Join(info.PgData, "recovery.signal"),
			[]byte(""),
			0o600)
	}

	// We need to generate a recovery.conf
	return os.WriteFile(
		path.Join(info.PgData, "recovery.conf"),
		[]byte(recoveryFileContents),
		0o600)
}

// GetEnforcedParametersThroughPgControldata will parse the output of pg_controldata in order to get
// the values of all the hot standby sensible parameters
func GetEnforcedParametersThroughPgControldata(pgData string) (map[string]string, error) {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	pgControlDataCmd := exec.Command(pgControlDataName,
		"-D",
		pgData) // #nosec G204
	pgControlDataCmd.Stdout = &stdoutBuffer
	pgControlDataCmd.Stderr = &stderrBuffer
	pgControlDataCmd.Env = append(pgControlDataCmd.Env, "LANG=C", "LC_MESSAGES=C")
	err := pgControlDataCmd.Run()
	if err != nil {
		log.Error(err, "while reading pg_controldata",
			"stderr", stderrBuffer.String(),
			"stdout", stdoutBuffer.String())
		return nil, err
	}

	log.Debug("pg_controldata stdout", "stdout", stdoutBuffer.String())

	enforcedParams := map[string]string{}
	for key, value := range utils.ParsePgControldataOutput(stdoutBuffer.String()) {
		if param, ok := pgControldataSettingsToParamsMap[key]; ok {
			enforcedParams[param] = value
		}
	}
	return enforcedParams, nil
}

// WriteInitialPostgresqlConf resets the postgresql.conf that there is in the instance using
// a new bootstrapped instance as reference
func (info InitInfo) WriteInitialPostgresqlConf(cluster *apiv1.Cluster) error {
	if err := fileutils.EnsureDirectoryExists(postgresSpec.RecoveryTemporaryDirectory); err != nil {
		return err
	}

	tempDataDir, err := os.MkdirTemp(postgresSpec.RecoveryTemporaryDirectory, "datadir_")
	if err != nil {
		return fmt.Errorf("while creating a temporary data directory: %w", err)
	}
	defer func() {
		err = os.RemoveAll(tempDataDir)
		if err != nil {
			log.Error(
				err,
				"skipping error while deleting temporary data directory")
		}
	}()

	temporaryInitInfo := InitInfo{
		PgData:    tempDataDir,
		Temporary: true,
	}

	if err = temporaryInitInfo.CreateDataDirectory(); err != nil {
		return fmt.Errorf("while creating a temporary data directory: %w", err)
	}

	temporaryInstance := temporaryInitInfo.GetInstance()
	temporaryInstance.Namespace = info.Namespace
	temporaryInstance.ClusterName = info.ClusterName

	_, err = temporaryInstance.RefreshPGHBA(cluster, "")
	if err != nil {
		return fmt.Errorf("while reading configuration files from ConfigMap: %w", err)
	}
	_, err = temporaryInstance.RefreshConfigurationFilesFromCluster(cluster, false)
	if err != nil {
		return fmt.Errorf("while reading configuration files from ConfigMap: %w", err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, "postgresql.conf"),
		path.Join(info.PgData, "postgresql.conf"))
	if err != nil {
		return fmt.Errorf("while creating postgresql.conf: %w", err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, constants.PostgresqlCustomConfigurationFile),
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile))
	if err != nil {
		return fmt.Errorf("while creating custom.conf: %w", err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, "postgresql.auto.conf"),
		path.Join(info.PgData, "postgresql.auto.conf"))
	if err != nil {
		return fmt.Errorf("while creating postgresql.auto.conf: %w", err)
	}

	// Disable SSL as we still don't have the required certificates
	err = fileutils.AppendStringToFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
		"ssl = 'off'\n")
	if err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	return err
}

// WriteRestoreHbaConf writes a pg_hba.conf allowing access without password from localhost.
// this is needed to set the PostgreSQL password after the postgres server is started and active
func (info InitInfo) WriteRestoreHbaConf() error {
	// We allow every access from localhost, and this is needed to correctly restore
	// the database
	_, err := fileutils.WriteStringToFile(
		path.Join(info.PgData, constants.PostgresqlHBARulesFile),
		"local all all peer map=local\n")
	if err != nil {
		return err
	}

	// Create the local map referred in the HBA configuration
	return WritePostgresUserMaps(info.PgData)
}

// ConfigureInstanceAfterRestore changes the superuser password
// of the instance to be coherent with the one specified in the
// cluster. This function also ensures that we can really connect
// to this cluster using the password in the secrets
func (info InitInfo) ConfigureInstanceAfterRestore(ctx context.Context, cluster *apiv1.Cluster, env []string) error {
	contextLogger := log.FromContext(ctx)

	instance := info.GetInstance()
	instance.Env = env

	if err := instance.VerifyPgDataCoherence(ctx); err != nil {
		contextLogger.Error(err, "while ensuring pgData coherence")
		return err
	}

	majorVersion, err := postgresutils.GetMajorVersion(info.PgData)
	if err != nil {
		return fmt.Errorf("cannot detect major version: %w", err)
	}

	// This will start the recovery of WALs taken during the backup
	// and, after that, the server will start in a new timeline
	if err = instance.WithActiveInstance(func() error {
		db, err := instance.GetSuperUserDB()
		if err != nil {
			return err
		}

		// Wait until we exit from recovery mode
		err = waitUntilRecoveryFinishes(db)
		if err != nil {
			return fmt.Errorf("while waiting for PostgreSQL to stop recovery mode: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	if majorVersion >= 12 {
		primaryConnInfo := info.GetPrimaryConnInfo()
		slotName := cluster.GetSlotNameFromInstanceName(info.PodName)
		_, err = configurePostgresAutoConfFile(info.PgData, primaryConnInfo, slotName)
		if err != nil {
			return fmt.Errorf("while configuring replica: %w", err)
		}
	}

	if info.ApplicationUser == "" || info.ApplicationDatabase == "" {
		log.Debug("configure new instance not ran, cluster is running in replica mode or missing user or database")
		return nil
	}

	// Configure the application database information for restored instance
	return instance.WithActiveInstance(func() error {
		err = info.ConfigureNewInstance(instance)
		if err != nil {
			return fmt.Errorf("while configuring restored instance: %w", err)
		}

		return nil
	})
}

// GetPrimaryConnInfo returns the DSN to reach the primary
func (info InitInfo) GetPrimaryConnInfo() string {
	return buildPrimaryConnInfo(info.ClusterName+"-rw", info.PodName)
}

func (info *InitInfo) checkBackupDestination(
	ctx context.Context,
	client client.Client,
	cluster *apiv1.Cluster,
) error {
	if !cluster.Spec.Backup.IsBarmanBackupConfigured() {
		return nil
	}
	// Get environment from cache
	env, err := barmanCredentials.EnvSetRestoreCloudCredentials(ctx,
		client,
		cluster.Namespace,
		cluster.Spec.Backup.BarmanObjectStore,
		os.Environ())
	if err != nil {
		return fmt.Errorf("can't get credentials for cluster %v: %w", cluster.Name, err)
	}
	if len(env) == 0 {
		return nil
	}

	// Instantiate the WALArchiver to get the proper configuration
	var walArchiver *archiver.WALArchiver
	walArchiver, err = archiver.New(ctx, cluster, env, walarchive.SpoolDirectory, info.PgData)
	if err != nil {
		return fmt.Errorf("while creating the archiver: %w", err)
	}

	// Get WAL archive options
	checkWalOptions, err := walArchiver.BarmanCloudCheckWalArchiveOptions(cluster, cluster.Name)
	if err != nil {
		log.Error(err, "while getting barman-cloud-wal-archive options")
		return err
	}

	// Check if we're ok to archive in the desired destination
	if utils.IsEmptyWalArchiveCheckEnabled(&cluster.ObjectMeta) {
		return walArchiver.CheckWalArchiveDestination(ctx, checkWalOptions)
	}

	return nil
}

// waitUntilRecoveryFinishes periodically checks the underlying
// PostgreSQL connection and returns only when the recovery
// mode is finished
func waitUntilRecoveryFinishes(db *sql.DB) error {
	errorIsRetriable := func(err error) bool {
		return err == ErrInstanceInRecovery
	}

	return retry.OnError(RetryUntilRecoveryDone, errorIsRetriable, func() error {
		row := db.QueryRow("SELECT pg_is_in_recovery()")

		var status bool
		if err := row.Scan(&status); err != nil {
			return fmt.Errorf("error while reading results of pg_is_in_recovery: %w", err)
		}

		log.Info("Checking if the server is still in recovery",
			"recovery", status)

		if status {
			return ErrInstanceInRecovery
		}

		return nil
	})
}
