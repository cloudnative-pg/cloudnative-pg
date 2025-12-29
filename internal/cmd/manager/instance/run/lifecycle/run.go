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

package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

var identifierStreamingReplicationUser = pgx.Identifier{apiv1.StreamingReplicationUser}.Sanitize()

// runPostgresAndWait runs a goroutine which will run, configure and run Postgres itself,
// returning any error via the returned channel
func (i *PostgresLifecycle) runPostgresAndWait(ctx context.Context) <-chan error {
	contextLogger := log.FromContext(ctx)

	runPostmasterSession := func() error {
		// Meanwhile PostgreSQL is starting, we'll start a goroutine
		// that will configure its permission once the database system
		// is ready to accept connection.
		//
		// This wait group ensures this goroutine to be finished when
		// this function exits
		var wg sync.WaitGroup
		defer wg.Wait()

		// We're creating a new Context for PostgreSQL, that will be cancelled
		// as soon as the postmaster exits.
		// The cancellation of this context will trigger the termination
		// of the goroutine initialization function.
		postgresContext, postgresContextCancel := context.WithCancel(ctx)
		defer postgresContextCancel()

		// Before starting the postmaster, we ensure we've the correct
		// permissions and user maps to start it.
		err := i.instance.VerifyPgDataCoherence(postgresContext)
		if err != nil {
			return err
		}

		// Here we need to wait for initialization to be executed before
		// being able to start the instance. Once this is done, we've executed
		// the first part of the instance reconciliation loop that don't need
		// a postmaster to be ready.
		// That part of the reconciliation loop ensures the PGDATA contains
		// the correct signal files to start in the correct replication role,
		// being a primary or a replica.
		//
		// If we come here because PostgreSQL have been restarted or because
		// fencing was lifted, this condition will be already met and the
		// following will be a no-op.
		i.systemInitialization.Wait()

		// If the system initialization failed, we return an error and let
		// the instance manager quit.
		if i.systemInitialization.Err() != nil {
			return err
		}

		// The lifecycle loop will call us even when PostgreSQL is fenced.
		// In that case there's no need to proceed.
		if i.instance.IsFenced() {
			contextLogger.Info("Instance is fenced, won't start postgres right now")
			return nil
		}

		i.instance.LogPgControldata(postgresContext, "postmaster start up")
		defer i.instance.LogPgControldata(postgresContext, "postmaster has exited")

		streamingCmd, err := i.instance.Run()
		if err != nil {
			contextLogger.Error(err, "Unable to start PostgreSQL up")
			return err
		}

		postMasterPID, err := streamingCmd.Pid()
		if err != nil {
			contextLogger.Error(
				err,
				"Programmatic error: postmaster process was not set")
			return err
		}

		contextLogger.Info("postmaster started", "postMasterPID", postMasterPID)

		// Now we'll wait for PostgreSQL to accept connections, and setup everything required
		// for replication and pg_rewind to work correctly.
		wg.Go(func() {
			if err := configureInstancePermissions(postgresContext, i.instance); err != nil {
				contextLogger.Error(err, "Unable to update PostgreSQL roles and permissions")
			}
		})

		// From now on the instance can be checked for readiness. This is
		// used from the readiness probe to avoid testing PostgreSQL.
		i.instance.SetCanCheckReadiness(true)
		defer i.instance.SetCanCheckReadiness(false)

		postmasterExitStatus := streamingCmd.Wait()
		contextLogger.Info(
			"postmaster exited",
			"postmasterExitStatus", postmasterExitStatus,
			"postMasterPID", postMasterPID,
		)
		return postmasterExitStatus
	}

	errChan := make(chan error, 1)

	// The following goroutine runs the postmaster process, and stops
	// when the process exits.
	go func() {
		errChan <- runPostmasterSession()
		close(errChan)
	}()

	return errChan
}

// ConfigureInstancePermissions creates the expected users and databases in a new
// PostgreSQL instance
func configureInstancePermissions(ctx context.Context, instance *postgres.Instance) error {
	contextLogger := log.FromContext(ctx)
	var err error
	isPrimary, err := instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		return nil
	}

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting a connection to the instance: %w", err)
	}

	contextLogger.Debug("Verifying connection to DB")
	if err := instance.WaitForSuperuserConnectionAvailable(ctx); err != nil {
		contextLogger.Error(err, "DB not available")
		return fmt.Errorf("while verifying super user DB connection: %w", err)
	}

	contextLogger.Debug("Validating DB configuration")

	// A transaction is required to temporarily disable synchronous replication
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("creating a new transaction to setup the instance: %w", err)
	}

	if err := configureStreamingReplicaUser(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err = configurePgRewindPrivileges(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// configureStreamingReplicaUser makes sure the streaming replication user exists
// and has the required rights
func configureStreamingReplicaUser(tx *sql.Tx) error {
	var hasLoginRight, hasReplicationRight bool
	row := tx.QueryRow("SELECT rolcanlogin, rolreplication FROM pg_catalog.pg_roles WHERE rolname = $1",
		apiv1.StreamingReplicationUser)
	err := row.Scan(&hasLoginRight, &hasReplicationRight)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("while getting streaming replication user privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"CREATE USER %v REPLICATION",
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("CREATE USER %v error: %w", apiv1.StreamingReplicationUser, err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"COMMENT ON ROLE %v IS 'Special user for streaming replication created by CloudNativePG'",
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("COMMENT ON ROLE %v error: %w", apiv1.StreamingReplicationUser, err)
		}
	}

	if !hasLoginRight || !hasReplicationRight {
		_, err = tx.Exec(fmt.Sprintf(
			"ALTER USER %v LOGIN REPLICATION",
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("ALTER USER %v error: %w", apiv1.StreamingReplicationUser, err)
		}
	}
	return nil
}

// configurePgRewindPrivileges ensures that the StreamingReplicationUser has enough rights to execute pg_rewind
func configurePgRewindPrivileges(tx *sql.Tx) error {
	// Ensure the user has rights to execute the functions needed for pg_rewind
	var hasPgRewindPrivileges bool
	row := tx.QueryRow(
		`
		SELECT pg_catalog.has_function_privilege($1,
				'pg_catalog.pg_ls_dir(text, boolean, boolean)', 'execute') AND
			pg_catalog.has_function_privilege($2,
				'pg_catalog.pg_stat_file(text, boolean)', 'execute') AND
			pg_catalog.has_function_privilege($3,
				'pg_catalog.pg_read_binary_file(text)', 'execute') AND
			pg_catalog.has_function_privilege($4,
				'pg_catalog.pg_read_binary_file(text, bigint, bigint, boolean)', 'execute')`,
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
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_stat_file(text, boolean) TO %v",
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text) TO %v",
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text, bigint, bigint, boolean) TO %v",
			identifierStreamingReplicationUser))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}
	}

	return nil
}
