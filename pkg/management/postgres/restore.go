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
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	barmanArchiver "github.com/cloudnative-pg/barman-cloud/pkg/archiver"
	barmanCatalog "github.com/cloudnative-pg/barman-cloud/pkg/catalog"
	barmanCommand "github.com/cloudnative-pg/barman-cloud/pkg/command"
	barmanCredentials "github.com/cloudnative-pg/barman-cloud/pkg/credentials"
	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	barmanUtils "github.com/cloudnative-pg/barman-cloud/pkg/utils"
	"github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
	"github.com/cloudnative-pg/machinery/pkg/envmap"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
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
// nolint:gocognit,gocyclo
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

	// We're creating a new replica of an existing cluster, and the PVCs
	// have been initialized by a set of VolumeSnapshots.
	if immediate {
		// If the instance starts as a primary, we will enter in the
		// same logic attaching an old primary back after a failover.
		// We don't need that as this instance has never diverged.
		if err := info.GetInstance(nil).Demote(ctx, cluster); err != nil {
			return fmt.Errorf("error while demoting the instance: %w", err)
		}
		return nil
	}

	// We're creating a new cluster from a snapshot backup, but we
	// have no recovery section defined. This is not possible.
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		return fmt.Errorf("missing snapshot recovery stanza in cluster .spec.bootstrap")
	}

	// We've no WAL archive, so we can't proceed with a PITR
	if cluster.Spec.Bootstrap.Recovery.Source == "" {
		return nil
	}

	contextLogger.Info("Recovering from volume snapshot",
		"sourceName", cluster.Spec.Bootstrap.Recovery.Source)

	if len(info.BackupLabelFile) > 0 {
		filePath := filepath.Join(info.PgData, constants.BackupLabelFile)
		if _, err := fileutils.WriteFileAtomic(filePath, info.BackupLabelFile, 0o666); err != nil {
			return err
		}
	}

	if len(info.TablespaceMapFile) > 0 {
		filePath := filepath.Join(info.PgData, constants.TablespaceMapFile)
		if _, err := fileutils.WriteFileAtomic(filePath, info.TablespaceMapFile, 0o666); err != nil {
			return err
		}
	}

	var envs []string
	restoreCmd := fmt.Sprintf(
		"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
		postgresSpec.LogPath, postgresSpec.LogFileName)
	config := fmt.Sprintf(
		"recovery_target_action = promote\n"+
			"restore_command = '%s'\n",
		restoreCmd)

	// nolint:nestif
	if pluginConfiguration := cluster.GetRecoverySourcePlugin(); pluginConfiguration == nil {
		envs, config, err = info.createEnvAndConfigForSnapshotRestore(ctx, cli, cluster)
		if err != nil {
			return err
		}
	}

	if _, err := info.restoreCustomWalDir(ctx); err != nil {
		return err
	}

	return info.concludeRestore(ctx, cli, cluster, config, envs)
}

func (info InitInfo) concludeRestore(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	config string,
	envs []string,
) error {
	if err := info.WriteInitialPostgresqlConf(ctx, cluster); err != nil {
		return err
	}
	filePath := filepath.Join(info.PgData, constants.CheckEmptyWalArchiveFile)
	// We create the check empty wal archive file to tell that we should check if the
	// destination path is empty
	if err := fileutils.CreateEmptyFile(filePath); err != nil {
		return fmt.Errorf("could not create %v file: %w", filePath, err)
	}

	if cluster.IsReplica() {
		server, ok := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)
		if !ok {
			return fmt.Errorf("missing external cluster: %v", cluster.Spec.ReplicaCluster.Source)
		}

		connectionString, err := external.ConfigureConnectionToServer(
			ctx, cli, info.Namespace, &server)
		if err != nil {
			return err
		}

		// TODO: Using a replication slot on replica cluster is not supported (yet?)
		_, err = UpdateReplicaConfiguration(info.PgData, connectionString, "")
		return err
	}

	if err := info.WriteRestoreHbaConf(ctx); err != nil {
		return err
	}

	if err := info.writeCustomRestoreWalConfig(cluster, config); err != nil {
		return err
	}

	return info.ConfigureInstanceAfterRestore(ctx, cluster, envs)
}

// createEnvAndConfigForSnapshotRestore creates env and config for snapshot restore
func (info InitInfo) createEnvAndConfigForSnapshotRestore(
	ctx context.Context,
	typedClient client.Client,
	cluster *apiv1.Cluster,
) ([]string, string, error) {
	contextLogger := log.FromContext(ctx)
	sourceName := cluster.Spec.Bootstrap.Recovery.Source

	if sourceName == "" {
		return nil, "", fmt.Errorf("recovery source not specified")
	}

	contextLogger.Info("Recovering from external cluster", "sourceName", sourceName)

	server, found := cluster.ExternalCluster(sourceName)
	if !found {
		return nil, "", fmt.Errorf("missing external cluster: %v", sourceName)
	}
	serverName := server.GetServerName()

	env, err := barmanCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		typedClient,
		cluster.Namespace,
		server.BarmanObjectStore,
		os.Environ())
	if err != nil {
		return nil, "", err
	}

	backup := &apiv1.Backup{
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
	}

	config, err := getRestoreWalConfig(ctx, backup)
	return env, config, err
}

// Restore restores a PostgreSQL cluster from a backup into the object storage
func (info InitInfo) Restore(ctx context.Context, cli client.Client) error {
	contextLogger := log.FromContext(ctx)

	cluster, err := info.loadCluster(ctx, cli)
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

	var envs []string
	var config string

	// nolint:nestif
	if pluginConfiguration := cluster.GetRecoverySourcePlugin(); pluginConfiguration != nil {
		contextLogger.Info("Restore through plugin detected, proceeding...")
		res, err := restoreViaPlugin(ctx, cluster, pluginConfiguration)
		if err != nil {
			return err
		}
		if res == nil {
			return errors.New("empty response from restoreViaPlugin, programmatic error")
		}

		processEnvironment, err := envmap.ParseEnviron()
		if err != nil {
			return fmt.Errorf("error while parsing the process environment: %w", err)
		}

		pluginEnvironment, err := envmap.Parse(res.Envs)
		if err != nil {
			return fmt.Errorf("error while parsing the plugin environment: %w", err)
		}

		envs = envmap.Merge(processEnvironment, pluginEnvironment).StringSlice()
		config = res.RestoreConfig
	} else {
		// Before starting the restore we check if the archive destination is safe to use
		// otherwise, we stop creating the cluster
		err = info.checkBackupDestination(ctx, cli, cluster)
		if err != nil {
			return err
		}

		// If we need to download data from a backup, we do it
		backup, env, err := info.loadBackup(ctx, cli, cluster)
		if err != nil {
			return err
		}

		if err := info.ensureArchiveContainsLastCheckpointRedoWAL(ctx, cluster, env, backup); err != nil {
			return err
		}

		if err := info.restoreDataDir(ctx, backup, env); err != nil {
			return err
		}

		if _, err := info.restoreCustomWalDir(ctx); err != nil {
			return err
		}

		conf, err := getRestoreWalConfig(ctx, backup)
		if err != nil {
			return err
		}
		config = conf
		envs = env
	}

	return info.concludeRestore(ctx, cli, cluster, config, envs)
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
			contextLogger.Error(err, "while deleting the temporary wal file")
		}
	}()

	if err := fileutils.EnsureParentDirectoryExists(testWALPath); err != nil {
		return err
	}

	rest, err := barmanRestorer.New(ctx, env, postgresSpec.SpoolDirectory)
	if err != nil {
		return err
	}

	opts, err := barmanCommand.CloudWalRestoreOptions(ctx, &apiv1.BarmanObjectStoreConfiguration{
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
	pgDataWal := path.Join(info.PgData, pgWalDirectory)

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
func (info InitInfo) restoreDataDir(ctx context.Context, backup *apiv1.Backup, env []string) error {
	contextLogger := log.FromContext(ctx)
	var options []string

	if backup.Status.EndpointURL != "" {
		options = append(options, "--endpoint-url", backup.Status.EndpointURL)
	}
	options = append(options, backup.Status.DestinationPath)
	options = append(options, backup.Status.ServerName)
	options = append(options, backup.Status.BackupID)

	options, err := barmanCommand.AppendCloudProviderOptionsFromBackup(ctx, options, backup.Status.BarmanCredentials)
	if err != nil {
		return err
	}

	options = append(options, info.PgData)

	contextLogger.Info("Starting barman-cloud-restore",
		"options", options)

	cmd := exec.Command(barmanUtils.BarmanCloudRestore, options...) // #nosec G204
	cmd.Env = env
	err = execlog.RunStreaming(cmd, barmanUtils.BarmanCloudRestore)
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			err = barmanCommand.UnmarshalBarmanCloudRestoreExitCode(exitError.ExitCode())
		}

		contextLogger.Error(err, "Can't restore backup")
		return err
	}
	contextLogger.Info("Restore completed")
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
	contextLogger := log.FromContext(ctx)
	sourceName := cluster.Spec.Bootstrap.Recovery.Source

	if sourceName == "" {
		return nil, nil, fmt.Errorf("recovery source not specified")
	}

	contextLogger.Info("Recovering from external cluster", "sourceName", sourceName)

	server, found := cluster.ExternalCluster(sourceName)
	if !found {
		return nil, nil, fmt.Errorf("missing external cluster: %v", sourceName)
	}

	if server.BarmanObjectStore == nil {
		return nil, nil, fmt.Errorf("missing barman object store configuration for source: %v", sourceName)
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

	backupCatalog, err := barmanCommand.GetBackupList(ctx, server.BarmanObjectStore, serverName, env)
	if err != nil {
		return nil, nil, err
	}

	// We are now choosing the right backup to restore
	var targetBackup *barmanCatalog.BarmanBackup
	if cluster.Spec.Bootstrap.Recovery != nil &&
		cluster.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
		targetBackup, err = backupCatalog.FindBackupInfo(
			cluster.Spec.Bootstrap.Recovery.RecoveryTarget,
		)
		if err != nil {
			return nil, nil, err
		}
	} else {
		targetBackup = backupCatalog.LatestBackupInfo()
	}
	if targetBackup == nil {
		return nil, nil, fmt.Errorf("no target backup found")
	}

	contextLogger.Info("Target backup found", "backup", targetBackup)

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
	contextLogger := log.FromContext(ctx)
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

	contextLogger.Info("Recovering existing backup", "backup", backup)
	return &backup, env, nil
}

func (info InitInfo) writeCustomRestoreWalConfig(cluster *apiv1.Cluster, conf string) error {
	recoveryFileContents := fmt.Sprintf(
		"%s\n"+
			"%s",
		conf,
		cluster.Spec.Bootstrap.Recovery.RecoveryTarget.BuildPostgresOptions())

	return info.writeRecoveryConfiguration(cluster, recoveryFileContents)
}

// getRestoreWalConfig obtains the content to append to `custom.conf` allowing PostgreSQL
// to complete the WAL recovery from the object storage and then start
// as a new primary
func getRestoreWalConfig(ctx context.Context, backup *apiv1.Backup) (string, error) {
	var err error

	cmd := []string{barmanUtils.BarmanCloudWalRestore}
	if backup.Status.EndpointURL != "" {
		cmd = append(cmd, "--endpoint-url", backup.Status.EndpointURL)
	}
	cmd = append(cmd, backup.Status.DestinationPath)
	cmd = append(cmd, backup.Status.ServerName)

	cmd, err = barmanCommand.AppendCloudProviderOptionsFromBackup(
		ctx, cmd, backup.Status.BarmanCredentials)
	if err != nil {
		return "", err
	}

	cmd = append(cmd, "%f", "%p")

	recoveryFileContents := fmt.Sprintf(
		"recovery_target_action = promote\n"+
			"restore_command = '%s'\n",
		strings.Join(cmd, " "))

	return recoveryFileContents, nil
}

func (info InitInfo) writeRecoveryConfiguration(cluster *apiv1.Cluster, recoveryFileContents string) error {
	// Ensure restore_command is used to correctly recover WALs
	// from the object storage

	log.Info("Generated recovery configuration", "configuration", recoveryFileContents)
	// Temporarily suspend WAL archiving. We set it to `false` (which means failure
	// of the archiver) in order to defer the decision about archiving to PostgreSQL
	// itself once the recovery job is completed and the instance is regularly started.
	err := fileutils.AppendStringToFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
		"archive_command = 'false'\n")
	if err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	// Now we need to choose which parameters to use to complete the recovery
	// of this PostgreSQL instance.
	// We know the values that these parameters had when the backup was started
	// from the `pg_controldata` output.
	// We don't know how these values were set in the newer WALs.
	//
	// The only way to proceed is to rely on the user-defined configuration,
	// with the caveat of ensuring that the values are high enough to be
	// able to start recovering the backup.
	//
	// To be on the safe side, we'll use the largest setting we find
	// from `pg_controldata` and the Cluster definition.
	//
	// https://www.postgresql.org/docs/16/hot-standby.html#HOT-STANDBY-ADMIN
	controldataParams, err := LoadEnforcedParametersFromPgControldata(info.PgData)
	if err != nil {
		return err
	}
	clusterParams, err := LoadEnforcedParametersFromCluster(cluster)
	if err != nil {
		return err
	}
	enforcedParams := make(map[string]string)
	for _, param := range pgControldataSettingsToParamsMap {
		value := max(clusterParams[param], controldataParams[param])
		enforcedParams[param] = strconv.Itoa(value)
	}
	changed, err := configfile.UpdatePostgresConfigurationFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
		enforcedParams,
	)
	if changed {
		log.Info(
			"Aligned PostgreSQL configuration to satisfy both pg_controldata and cluster spec",
			"enforcedParams", enforcedParams,
			"controldataParams", controldataParams,
			"clusterParams", clusterParams,
		)
	}
	if err != nil {
		return fmt.Errorf("cannot write recovery config for enforced parameters: %w", err)
	}

	// Append restore_command to the end of the
	// custom configs file
	err = fileutils.AppendStringToFile(
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile),
		recoveryFileContents)
	if err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	err = os.WriteFile(
		path.Join(info.PgData, constants.PostgresqlOverrideConfigurationFile),
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

// LoadEnforcedParametersFromPgControldata will parse the output of pg_controldata in order to get
// the values of all the hot standby sensible parameters
func LoadEnforcedParametersFromPgControldata(pgData string) (map[string]int, error) {
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

	enforcedParams := make(map[string]int)
	for key, value := range utils.ParsePgControldataOutput(stdoutBuffer.String()) {
		if param, ok := pgControldataSettingsToParamsMap[key]; ok {
			intValue, err := strconv.Atoi(value)
			if err != nil {
				log.Error(err, "while parsing pg_controldata content",
					"key", key,
					"value", value)
				return nil, err
			}
			enforcedParams[param] = intValue
		}
	}

	return enforcedParams, nil
}

// LoadEnforcedParametersFromCluster loads the enforced parameters which defined in cluster spec
func LoadEnforcedParametersFromCluster(
	cluster *apiv1.Cluster,
) (map[string]int, error) {
	clusterParams := cluster.Spec.PostgresConfiguration.Parameters
	enforcedParams := map[string]int{}
	for _, param := range pgControldataSettingsToParamsMap {
		value, found := clusterParams[param]
		if !found {
			continue
		}

		intValue, err := strconv.Atoi(value)
		if err != nil {
			log.Error(err, "while parsing enforced postgres parameter",
				"param", param,
				"value", value)
			return nil, err
		}
		enforcedParams[param] = intValue
	}
	return enforcedParams, nil
}

// WriteInitialPostgresqlConf resets the postgresql.conf that there is in the instance using
// a new bootstrapped instance as reference
func (info InitInfo) WriteInitialPostgresqlConf(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
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
			contextLogger.Error(
				err,
				"skipping error while deleting temporary data directory")
		}
	}()

	enabledPluginNamesSet := stringset.From(cluster.GetJobEnabledPluginNames())
	pluginCli, err := pluginClient.NewClient(ctx, enabledPluginNamesSet)
	if err != nil {
		return fmt.Errorf("error while creating the plugin client: %w", err)
	}
	defer pluginCli.Close(ctx)
	ctx = pluginClient.SetPluginClientInContext(ctx, pluginCli)
	ctx = cluster.SetInContext(ctx)

	temporaryInitInfo := InitInfo{
		PgData:    tempDataDir,
		Temporary: true,
	}

	if err = temporaryInitInfo.CreateDataDirectory(); err != nil {
		return fmt.Errorf("while creating a temporary data directory: %w", err)
	}

	temporaryInstance := temporaryInitInfo.GetInstance(cluster).
		WithNamespace(info.Namespace).
		WithClusterName(info.ClusterName)

	_, err = temporaryInstance.RefreshPGHBA(ctx, cluster, "")
	if err != nil {
		return fmt.Errorf("while generating pg_hba.conf: %w", err)
	}
	_, err = temporaryInstance.RefreshPGIdent(ctx, cluster.Spec.PostgresConfiguration.PgIdent)
	if err != nil {
		return fmt.Errorf("while generating pg_ident.conf: %w", err)
	}
	_, err = temporaryInstance.RefreshConfigurationFilesFromCluster(
		ctx,
		cluster,
		false,
		postgres.OperationType_TYPE_RESTORE,
	)
	if err != nil {
		return fmt.Errorf("while generating Postgres configuration: %w", err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, "postgresql.conf"),
		path.Join(info.PgData, "postgresql.conf"))
	if err != nil {
		return fmt.Errorf("while installing postgresql.conf: %w", err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, constants.PostgresqlCustomConfigurationFile),
		path.Join(info.PgData, constants.PostgresqlCustomConfigurationFile))
	if err != nil {
		return fmt.Errorf("while installing %v: %w", constants.PostgresqlCustomConfigurationFile, err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, constants.PostgresqlOverrideConfigurationFile),
		path.Join(info.PgData, constants.PostgresqlOverrideConfigurationFile))
	if err != nil {
		return fmt.Errorf("while installing %v: %w", constants.PostgresqlOverrideConfigurationFile, err)
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

// WriteRestoreHbaConf writes basic pg_hba.conf and pg_ident.conf allowing access without password from localhost.
// This is needed to set the PostgreSQL password after the postgres server is started and active
func (info InitInfo) WriteRestoreHbaConf(ctx context.Context) error {
	// We allow every access from localhost, and this is needed to correctly restore
	// the database
	_, err := fileutils.WriteStringToFile(
		path.Join(info.PgData, constants.PostgresqlHBARulesFile),
		"local all all peer map=local\n")
	if err != nil {
		return err
	}

	// Create only the local map referred in the HBA configuration
	_, err = info.GetInstance(nil).RefreshPGIdent(ctx, nil)
	return err
}

// ConfigureInstanceAfterRestore changes the superuser password
// of the instance to be coherent with the one specified in the
// cluster. This function also ensures that we can really connect
// to this cluster using the password in the secrets
func (info InitInfo) ConfigureInstanceAfterRestore(ctx context.Context, cluster *apiv1.Cluster, env []string) error {
	contextLogger := log.FromContext(ctx)

	instance := info.GetInstance(cluster)
	instance.Env = env

	if err := instance.VerifyPgDataCoherence(ctx); err != nil {
		contextLogger.Error(err, "while ensuring pgData coherence")
		return err
	}

	// This will start the recovery of WALs taken during the backup
	// and, after that, the server will start in a new timeline
	if err := instance.WithActiveInstance(func() error {
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

	primaryConnInfo := info.GetPrimaryConnInfo()
	slotName := cluster.GetSlotNameFromInstanceName(info.PodName)
	if _, err := configurePostgresOverrideConfFile(info.PgData, primaryConnInfo, slotName); err != nil {
		return fmt.Errorf("while configuring replica: %w", err)
	}

	if info.ApplicationUser == "" || info.ApplicationDatabase == "" {
		log.Debug("configure new instance not ran, cluster is running in replica mode or missing user or database")
		return nil
	}

	// Configure the application database information for restored instance
	return instance.WithActiveInstance(func() error {
		if err := info.ConfigureNewInstance(instance); err != nil {
			return fmt.Errorf("while configuring restored instance: %w", err)
		}

		return nil
	})
}

// GetPrimaryConnInfo returns the DSN to reach the primary
func (info InitInfo) GetPrimaryConnInfo() string {
	result := buildPrimaryConnInfo(info.ClusterName+"-rw", info.PodName) + " dbname=postgres"

	standbyTCPUserTimeout := os.Getenv("CNPG_STANDBY_TCP_USER_TIMEOUT")
	if len(standbyTCPUserTimeout) == 0 {
		// Default to 5000ms (5 seconds) if not explicitly set
		standbyTCPUserTimeout = "5000"
	}

	result = fmt.Sprintf("%s tcp_user_timeout='%s'", result,
		strings.ReplaceAll(strings.ReplaceAll(standbyTCPUserTimeout, `\`, `\\`), `'`, `\'`))

	return result
}

func (info *InitInfo) checkBackupDestination(
	ctx context.Context,
	client client.Client,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)
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
	var walArchiver *barmanArchiver.WALArchiver
	walArchiver, err = barmanArchiver.New(
		ctx,
		env,
		postgresSpec.SpoolDirectory,
		info.PgData,
		path.Join(info.PgData, constants.CheckEmptyWalArchiveFile))
	if err != nil {
		return fmt.Errorf("while creating the archiver: %w", err)
	}

	// Get WAL archive options
	checkWalOptions, err := walArchiver.BarmanCloudCheckWalArchiveOptions(
		ctx, cluster.Spec.Backup.BarmanObjectStore, cluster.Name)
	if err != nil {
		contextLogger.Error(err, "while getting barman-cloud-wal-archive options")
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
		row := db.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")

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

// restoreViaPlugin tries to restore the cluster using a plugin if available and enabled.
// Returns true if a restore plugin was found and any error encountered.
func restoreViaPlugin(
	ctx context.Context,
	cluster *apiv1.Cluster,
	plugin *apiv1.PluginConfiguration,
) (*restore.RestoreResponse, error) {
	contextLogger := log.FromContext(ctx)

	plugins := repository.New()
	defer plugins.Close()

	pluginEnabledSet := stringset.New()
	pluginEnabledSet.Put(plugin.Name)
	pClient, err := pluginClient.NewClient(ctx, pluginEnabledSet)
	if err != nil {
		contextLogger.Error(err, "Error while loading required plugins")
		return nil, err
	}
	defer pClient.Close(ctx)

	return pClient.Restore(ctx, cluster)
}
