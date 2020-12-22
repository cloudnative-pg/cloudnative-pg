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
	"strings"
	"time"

	"github.com/lib/pq"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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
)

// Restore restore a PostgreSQL cluster from a backup into the object storage
func (info InitInfo) Restore() error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	backup, err := info.loadBackup()
	if err != nil {
		return err
	}

	if err := info.restoreDataDir(backup); err != nil {
		return err
	}

	if err := info.writeInitialPostgresqlConf(dynamicClient); err != nil {
		return err
	}

	if err := info.writeRestoreHbaConf(); err != nil {
		return err
	}

	if err := info.writeRestoreWalConfig(backup); err != nil {
		return err
	}

	return info.configureInstanceAfterRestore()
}

// restoreDataDir restore PGDATA from an existing backup
func (info InitInfo) restoreDataDir(backup *v1alpha1.Backup) error {
	options := []string{}
	if backup.Status.EndpointURL != "" {
		options = append(options, "--endpoint-url", backup.Status.EndpointURL)
	}
	if backup.Status.Encryption != "" {
		options = append(options, "-e", backup.Status.Encryption)
	}
	options = append(options, backup.Status.DestinationPath)
	options = append(options, backup.Status.ServerName)
	options = append(options, backup.Status.BackupID)
	options = append(options, info.PgData)

	log.Log.Info("Starting barman-cloud-restore",
		"options", options)

	cmd := exec.Command("barman-cloud-restore", options...) // #nosec G204
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err := cmd.Run()

	if err != nil {
		log.Log.Error(err, "Can't restore backup",
			"stdOut", stdoutBuffer.String(),
			"stdErr", stderrBuffer.String())
	} else {
		log.Log.Info("Restore completed", "output", err)
	}

	return err
}

// getBackupObjectKey construct the object key where the backup will be found
func (info InitInfo) getBackupObjectKey() client.ObjectKey {
	return client.ObjectKey{Namespace: info.Namespace, Name: info.BackupName}
}

// loadBackup loads the backup manifest from the API server
func (info InitInfo) loadBackup() (*v1alpha1.Backup, error) {
	typedClient, err := management.NewClient()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var backup v1alpha1.Backup
	err = typedClient.Get(ctx, info.getBackupObjectKey(), &backup)
	if err != nil {
		return nil, err
	}

	return &backup, nil
}

// writeRestoreWalConfig write a `custom.config` allowing PostgreSQL
// to complete the WAL recovery from the object storage and then start
// as a new master
func (info InitInfo) writeRestoreWalConfig(backup *v1alpha1.Backup) error {
	// Ensure restore_command is used to correctly recover WALs
	// from the object storage
	major, err := postgres.GetMajorVersion(info.PgData)
	if err != nil {
		return fmt.Errorf("cannot detect major version: %w", err)
	}

	cmd := []string{"barman-cloud-wal-restore"}
	if backup.Status.Encryption != "" {
		cmd = append(cmd, "-e", backup.Status.Encryption)
	}
	if backup.Status.EndpointURL != "" {
		cmd = append(cmd, "--endpoint-url", backup.Status.EndpointURL)
	}
	cmd = append(cmd, backup.Status.DestinationPath)
	cmd = append(cmd, backup.Spec.Cluster.Name)
	cmd = append(cmd, "%f", "%p")

	recoveryFileContents := fmt.Sprintf(
		"recovery_target_action = promote\n"+
			"restore_command = '%s'\n"+
			"%s",
		strings.Join(cmd, " "),
		info.RecoveryTarget)

	log.Log.Info("Generated recovery configuration", "configuration", recoveryFileContents)

	// Disable SSL as we still don't have the required certificates
	err = fileutils.AppendStringToFile(
		path.Join(info.PgData, "custom.conf"),
		"ssl = 'off'\n"+
			"archive_command = 'cd .'\n")
	if err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	if major >= 12 {
		// Append restore_command to the end of the
		// custom configs file
		err = fileutils.AppendStringToFile(
			path.Join(info.PgData, "custom.conf"),
			recoveryFileContents)
		if err != nil {
			return fmt.Errorf("cannot write recovery config: %w", err)
		}

		err = ioutil.WriteFile(
			path.Join(info.PgData, "postgresql.auto.conf"),
			[]byte(""),
			0600)
		if err != nil {
			return fmt.Errorf("cannot erase auto config: %w", err)
		}

		// Create recovery signal file
		return ioutil.WriteFile(
			path.Join(info.PgData, "recovery.signal"),
			[]byte(""),
			0600)
	}

	// We need to generate a recovery.conf
	return ioutil.WriteFile(
		path.Join(info.PgData, "recovery.conf"),
		[]byte(recoveryFileContents),
		0600)
}

// writeInitialPostgresqlConf reset the postgresql.conf that there is in the instance
func (info InitInfo) writeInitialPostgresqlConf(client dynamic.Interface) error {
	tempDataDir, err := ioutil.TempDir("/tmp", "datadir_")
	if err != nil {
		return fmt.Errorf("while creating a temporary data directory: %w", err)
	}
	defer func() {
		err = os.RemoveAll(tempDataDir)
		if err != nil {
			log.Log.Error(
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

	err = temporaryInstance.RefreshConfigurationFiles(client)
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
		path.Join(temporaryInitInfo.PgData, "custom.conf"),
		path.Join(info.PgData, "custom.conf"))
	if err != nil {
		return fmt.Errorf("while creating custom.conf: %w", err)
	}

	err = fileutils.CopyFile(
		path.Join(temporaryInitInfo.PgData, "postgresql.auto.conf"),
		path.Join(info.PgData, "postgresql.auto.conf"))
	if err != nil {
		return fmt.Errorf("while creating postgresql.auto.conf: %w", err)
	}

	return err
}

// writeRestoreHbaConf write a pg_hba.conf allowing access without password from localhost.
// this is needed to set the PostgreSQL password after the postmaster is started and active
func (info InitInfo) writeRestoreHbaConf() error {
	// We allow every access from localhost, and this is needed to correctly restore
	// the database
	temporaryHbaRules := "local all all peer"
	return ioutil.WriteFile(
		path.Join(info.PgData, PostgresqlHBARulesFile),
		[]byte(temporaryHbaRules),
		0600)
}

// configureInstanceAfterRestore change the superuser password
// of the instance to be coherent with the one specified in the
// cluster. This function also ensure that we can really connect
// to this cluster using the password in the secrets
func (info InitInfo) configureInstanceAfterRestore() error {
	superUserPassword, err := fileutils.ReadFile(info.PasswordFile)
	if err != nil {
		return fmt.Errorf("cannot read superUserPassword file: %w", err)
	}

	instance := info.GetInstance()

	majorVersion, err := postgres.GetMajorVersion(info.PgData)
	if err != nil {
		return fmt.Errorf("cannot detect major version: %w", err)
	}

	// This will start the recovery of WALs taken during the backup
	// and, after that, the server will start in a new timeline
	return instance.WithActiveInstance(func() error {
		db, err := instance.GetSuperUserDB()
		if err != nil {
			return err
		}

		// Wait until we exit from recovery mode
		err = waitUntilRecoveryFinishes(db)
		if err != nil {
			return fmt.Errorf("while waiting for PostgreSQL to stop recovery mode: %w", err)
		}

		_, err = db.Exec(fmt.Sprintf(
			"ALTER USER postgres PASSWORD %v",
			pq.QuoteLiteral(superUserPassword)))
		if err != nil {
			return fmt.Errorf("ALTER USER postgres error: %w", err)
		}

		if majorVersion >= 12 {
			return info.ConfigureReplica(db)
		}

		return nil
	})
}

// waitUntilRecoveryFinishes periodically check the underlying
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

		log.Log.Info("Checking if the server is still in recovery",
			"recovery", status)

		if status {
			return ErrInstanceInRecovery
		}

		return nil
	})
}
