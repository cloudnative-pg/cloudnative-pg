/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package run

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/lib/pq"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/concurrency"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metricsserver"
	postgresUtils "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// InstanceRunnable implements the manager.Runnable interface for an postgres.Instance
// TODO split in signalHandler (to be renamed)
type InstanceRunnable struct {
	instance *postgres.Instance

	ctx                  context.Context
	cancel               context.CancelFunc
	systemInitialization *concurrency.Executed
}

// NewInstanceRunnable creates a new InstanceRunnable
func NewInstanceRunnable(
	ctx context.Context,
	instance *postgres.Instance,
	inizialization *concurrency.Executed,
) *InstanceRunnable {
	ctx, cancel := context.WithCancel(ctx)
	return &InstanceRunnable{instance: instance, ctx: ctx, cancel: cancel, systemInitialization: inizialization}
}

// Start starts running the InstanceRunnable
// nolint:gocognit
func (i *InstanceRunnable) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx)

	err := VerifyPgDataCoherence(ctx, i.instance)
	if err != nil {
		return err
	}

	// here we need to wait for initialization to be finished before proceeding
	i.systemInitialization.Wait()

	i.instance.LogPgControldata()

	streamingCmd, err := i.instance.Run()
	if err != nil {
		contextLog.Error(err, "Unable to start PostgreSQL up")
		return err
	}

	err = configureInstancePermissions(i.instance)
	if err != nil {
		log.Error(err, "Unable to update PostgreSQL roles and permissions")
		return err
	}

	if err = streamingCmd.Wait(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			contextLog.Error(err, "Error waiting on PostgreSQL process")
		} else {
			contextLog.Error(exitError, "PostgreSQL process exited with errors")
		}
	}

	i.instance.LogPgControldata()

	i.cancel()

	return nil
}

// ConfigureInstancePermissions creates the expected users and databases in a new
// PostgreSQL instance
func configureInstancePermissions(instance *postgres.Instance) error {
	var err error
	isPrimary, err := instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		return nil
	}

	majorVersion, err := postgresUtils.GetMajorVersion(instance.PgData)
	if err != nil {
		return fmt.Errorf("while getting major version: %w", err)
	}

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting a connection to the instance: %w", err)
	}

	log.Debug("Verifying connection to DB")
	err = instance.WaitForSuperuserConnectionAvailable()
	if err != nil {
		log.Error(err, "DB not available")
		os.Exit(1)
	}

	log.Debug("Validating DB configuration")

	// A transaction is required to temporarily disable synchronous replication
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("creating a new transaction to setup the instance: %w", err)
	}

	_, err = tx.Exec("SET LOCAL synchronous_commit TO LOCAL")
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	hasSuperuser, err := configureStreamingReplicaUser(tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	err = configurePgRewindPrivileges(majorVersion, hasSuperuser, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// configureStreamingReplicaUser makes sure the the streaming replication user exists
// and has the required rights
func configureStreamingReplicaUser(tx *sql.Tx) (bool, error) {
	var hasLoginRight, hasReplicationRight, hasSuperuser bool
	row := tx.QueryRow("SELECT rolcanlogin, rolreplication, rolsuper FROM pg_roles WHERE rolname = $1",
		apiv1.StreamingReplicationUser)
	err := row.Scan(&hasLoginRight, &hasReplicationRight, &hasSuperuser)
	if err != nil {
		if err == sql.ErrNoRows {
			_, err = tx.Exec(fmt.Sprintf(
				"CREATE USER %v REPLICATION",
				pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
			if err != nil {
				return false, fmt.Errorf("CREATE USER %v error: %w", apiv1.StreamingReplicationUser, err)
			}
		} else {
			return false, fmt.Errorf("while creating streaming replication user: %w", err)
		}
	}

	if !hasLoginRight || !hasReplicationRight {
		_, err = tx.Exec(fmt.Sprintf(
			"ALTER USER %v LOGIN REPLICATION",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return false, fmt.Errorf("ALTER USER %v error: %w", apiv1.StreamingReplicationUser, err)
		}
	}
	return hasSuperuser, nil
}

// configurePgRewindPrivileges ensures that the StreamingReplicationUser has enough rights to execute pg_rewind
func configurePgRewindPrivileges(majorVersion int, hasSuperuser bool, tx *sql.Tx) error {
	// We need the superuser bit for the streaming-replication user since pg_rewind in PostgreSQL <= 10
	// will require it.
	if majorVersion <= 10 {
		if !hasSuperuser {
			_, err := tx.Exec(fmt.Sprintf(
				"ALTER USER %v SUPERUSER",
				pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
			if err != nil {
				return fmt.Errorf("ALTER USER %v error: %w", apiv1.StreamingReplicationUser, err)
			}
		}
		return nil
	}

	// Ensure the user has rights to execute the functions needed for pg_rewind
	var hasPgRewindPrivileges bool
	row := tx.QueryRow(
		`
			SELECT has_function_privilege($1, 'pg_ls_dir(text, boolean, boolean)', 'execute') AND
			       has_function_privilege($2, 'pg_stat_file(text, boolean)', 'execute') AND
			       has_function_privilege($3, 'pg_read_binary_file(text)', 'execute') AND
			       has_function_privilege($4, 'pg_read_binary_file(text, bigint, bigint, boolean)', 'execute')`,
		apiv1.StreamingReplicationUser,
		apiv1.StreamingReplicationUser,
		apiv1.StreamingReplicationUser,
		apiv1.StreamingReplicationUser)
	err := row.Scan(&hasPgRewindPrivileges)
	if err != nil {
		return fmt.Errorf("while getting streaming replication user privileges: %w", err)
	}

	if !hasPgRewindPrivileges {
		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_ls_dir(text, boolean, boolean) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_stat_file(text, boolean) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text, bigint, bigint, boolean) TO %v",
			pq.QuoteIdentifier(apiv1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}
	}

	return nil
}

// VerifyPgDataCoherence checks if this cluster exists in K8s. It panics if this
// pod belongs to a primary but the cluster status is not coherent with that
func VerifyPgDataCoherence(ctx context.Context, instance *postgres.Instance) error {
	contextLogger := log.FromContext(ctx)

	contextLogger.Debug("Checking PGDATA coherence")

	if err := fileutils.EnsurePgDataPerms(instance.PgData); err != nil {
		return err
	}

	if err := postgres.WritePostgresUserMaps(instance.PgData); err != nil {
		return err
	}

	return nil
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func (i *InstanceRunnable) setupSignalHandler() context.Context {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error

		sig := <-signals
		log.Info("Received termination signal", "signal", sig)

		// We need to shut down the postmaster in a certain time (dictated by `cluster.GetMaxStopDelay()`).
		// For half of this time, we are waiting for connections to go down, the other half
		// we just handle the shutdown procedure itself.
		smartShutdownTimeout := int(i.instance.MaxStopDelay) / 2

		log.Info("Shutting down the metrics server")
		err = metricsserver.Shutdown()
		if err != nil {
			log.Error(err, "Error while shutting down the metrics server")
		} else {
			log.Info("Metrics server shut down")
		}

		log.Info("Requesting smart shutdown of the PostgreSQL instance")
		err = i.instance.Shutdown(postgres.ShutdownOptions{
			Mode:    postgres.ShutdownModeSmart,
			Wait:    true,
			Timeout: &smartShutdownTimeout,
		})
		if err != nil {
			log.Warning("Error while handling the smart shutdown request: requiring fast shutdown",
				"err", err)
			err = i.instance.Shutdown(postgres.ShutdownOptions{
				Mode: postgres.ShutdownModeFast,
				Wait: true,
			})
		}
		if err != nil {
			log.Error(err, "Error while shutting down the PostgreSQL instance")
		} else {
			log.Info("PostgreSQL instance shut down")
		}

		i.cancel()
	}()

	return i.ctx
}
