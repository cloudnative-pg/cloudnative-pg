/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configfile"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	barmanCapabilities "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/capabilities"
	barmanCredentials "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman/credentials"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/execlog"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/external"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	postgresSpec "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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

	enforcedParametersRegex          = regexp.MustCompile(`(?P<PARAM>[a-z_]+) setting:\s+(?P<VALUE>[a-z0-9]+)`)
	pgControldataSettingsToParamsMap = map[string]string{
		"max_connections":      "max_connections",
		"max_wal_senders":      "max_wal_senders",
		"max_worker_processes": "max_worker_processes",
		"max_prepared_xacts":   "max_prepared_transactions",
		"max_locks_per_xact":   "max_locks_per_transaction",
	}
)

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

	backup, env, err := info.loadBackup(ctx, typedClient, cluster)
	if err != nil {
		return err
	}

	if err := info.restoreDataDir(backup, env); err != nil {
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

		_, err = UpdateReplicaConfigurationForPrimary(info.PgData, connectionString)
		return err
	}

	if err := info.WriteRestoreHbaConf(); err != nil {
		return err
	}

	if err := info.writeRestoreWalConfig(backup); err != nil {
		return err
	}

	return info.ConfigureInstanceAfterRestore(env)
}

// restoreDataDir restores PGDATA from an existing backup
func (info InitInfo) restoreDataDir(backup *apiv1.Backup, env []string) error {
	var options []string

	if backup.Status.EndpointURL != "" {
		options = append(options, "--endpoint-url", backup.Status.EndpointURL)
	}
	if backup.Status.Encryption != "" {
		options = append(options, "-e", backup.Status.Encryption)
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
	cluster *apiv1.Cluster) (*apiv1.Backup, []string, error) {
	sourceName := cluster.Spec.Bootstrap.Recovery.Source
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

	catalog, err := barman.GetBackupList(server.BarmanObjectStore, serverName, env)
	if err != nil {
		return nil, nil, err
	}

	// Here we are simply loading the latest backup from the object store but
	// since we have a catalog we could easily find the required backup to
	// restore given a certain point in time, and we have it into
	// cluster.Spec.Bootstrap.Recovery.RecoveryTarget.TargetTime
	//
	// TODO: we need to think about the API or just simply use the stated
	// property to do the job
	latestBackup := catalog.LatestBackupInfo()
	if latestBackup == nil {
		return nil, nil, fmt.Errorf("no backup found")
	}

	log.Info("Latest backup found", "backup", latestBackup)

	return &apiv1.Backup{
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: serverName,
			},
		},
		Status: apiv1.BackupStatus{
			S3Credentials:    server.BarmanObjectStore.S3Credentials,
			AzureCredentials: server.BarmanObjectStore.AzureCredentials,
			EndpointCA:       server.BarmanObjectStore.EndpointCA,
			EndpointURL:      server.BarmanObjectStore.EndpointURL,
			DestinationPath:  server.BarmanObjectStore.DestinationPath,
			ServerName:       serverName,
			BackupID:         latestBackup.ID,
			Phase:            apiv1.BackupPhaseCompleted,
			StartedAt:        &metav1.Time{Time: latestBackup.BeginTime},
			StoppedAt:        &metav1.Time{Time: latestBackup.EndTime},
			BeginWal:         latestBackup.BeginWal,
			EndWal:           latestBackup.EndWal,
			BeginLSN:         latestBackup.BeginLSN,
			EndLSN:           latestBackup.EndLSN,
			Error:            latestBackup.Error,
			CommandOutput:    "",
			CommandError:     "",
		},
	}, env, nil
}

// loadBackupFromReference loads a backup object and the required credentials given the backup object resource
func (info InitInfo) loadBackupFromReference(
	ctx context.Context,
	typedClient client.Client,
	cluster *apiv1.Cluster) (*apiv1.Backup, []string, error) {
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
			S3Credentials:    backup.Status.S3Credentials,
			AzureCredentials: backup.Status.AzureCredentials,
			EndpointCA:       backup.Status.EndpointCA,
			EndpointURL:      backup.Status.EndpointURL,
			DestinationPath:  backup.Status.DestinationPath,
			ServerName:       backup.Status.ServerName,
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
func (info InitInfo) writeRestoreWalConfig(backup *apiv1.Backup) error {
	// Ensure restore_command is used to correctly recover WALs
	// from the object storage
	major, err := postgresSpec.GetMajorVersion(info.PgData)
	if err != nil {
		return fmt.Errorf("cannot detect major version: %w", err)
	}

	const barmanCloudWalRestoreName = "barman-cloud-wal-restore"

	cmd := []string{barmanCloudWalRestoreName}
	if backup.Status.Encryption != "" {
		cmd = append(cmd, "-e", backup.Status.Encryption)
	}
	if backup.Status.EndpointURL != "" {
		cmd = append(cmd, "--endpoint-url", backup.Status.EndpointURL)
	}
	cmd = append(cmd, backup.Status.DestinationPath)
	cmd = append(cmd, backup.Spec.Cluster.Name)

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
		info.RecoveryTarget)

	log.Info("Generated recovery configuration", "configuration", recoveryFileContents)
	// Disable archiving
	err = fileutils.AppendStringToFile(
		path.Join(info.PgData, PostgresqlCustomConfigurationFile),
		"archive_command = 'cd .'\n")
	if err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	enforcedParams, err := getEnforcedParametersThroughPgControldata(info)
	if err != nil {
		return err
	}
	if enforcedParams != nil {
		changed, err := configfile.UpdatePostgresConfigurationFile(
			path.Join(info.PgData, PostgresqlCustomConfigurationFile),
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
			path.Join(info.PgData, PostgresqlCustomConfigurationFile),
			recoveryFileContents)
		if err != nil {
			return fmt.Errorf("cannot write recovery config: %w", err)
		}

		err = ioutil.WriteFile(
			path.Join(info.PgData, "postgresql.auto.conf"),
			[]byte(""),
			0o600)
		if err != nil {
			return fmt.Errorf("cannot erase auto config: %w", err)
		}

		// Create recovery signal file
		return ioutil.WriteFile(
			path.Join(info.PgData, "recovery.signal"),
			[]byte(""),
			0o600)
	}

	// We need to generate a recovery.conf
	return ioutil.WriteFile(
		path.Join(info.PgData, "recovery.conf"),
		[]byte(recoveryFileContents),
		0o600)
}

func getEnforcedParametersThroughPgControldata(info InitInfo) (map[string]string, error) {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	pgControlDataCmd := exec.Command(pgControlDataName, "-D",
		info.PgData) // #nosec G204
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
	for _, line := range strings.Split(stdoutBuffer.String(), "\n") {
		matches := enforcedParametersRegex.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}
		if param, ok := pgControldataSettingsToParamsMap[matches[1]]; ok {
			enforcedParams[param] = matches[2]
		}
	}
	return enforcedParams, nil
}

// WriteInitialPostgresqlConf resets the postgresql.conf that there is in the instance using
// a new bootstrapped instance as reference
func (info InitInfo) WriteInitialPostgresqlConf(cluster *apiv1.Cluster) error {
	if err := fileutils.EnsureDirectoryExist(postgresSpec.RecoveryTemporaryDirectory); err != nil {
		return err
	}

	tempDataDir, err := ioutil.TempDir(postgresSpec.RecoveryTemporaryDirectory, "datadir_")
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

	_, err = temporaryInstance.RefreshConfigurationFilesFromCluster(cluster)
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
		path.Join(temporaryInitInfo.PgData, PostgresqlCustomConfigurationFile),
		path.Join(info.PgData, PostgresqlCustomConfigurationFile))
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
		path.Join(info.PgData, PostgresqlCustomConfigurationFile),
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
		path.Join(info.PgData, PostgresqlHBARulesFile),
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
func (info InitInfo) ConfigureInstanceAfterRestore(env []string) error {
	instance := info.GetInstance()
	instance.Env = env

	majorVersion, err := postgresSpec.GetMajorVersion(info.PgData)
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
		primaryConnInfo := buildPrimaryConnInfo(info.ClusterName, info.PodName)
		_, err = configurePostgresAutoConfFile(info.PgData, primaryConnInfo)
		if err != nil {
			return fmt.Errorf("while configuring replica: %w", err)
		}
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
