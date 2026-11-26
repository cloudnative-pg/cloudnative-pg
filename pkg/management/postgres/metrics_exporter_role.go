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
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// metricsExporterDefaultDatabases are the databases the metrics exporter is
// granted CONNECT on when its role is created. The grant is not repeated
// afterwards, so that it can be revoked.
var metricsExporterDefaultDatabases = []string{"postgres", "template1"}

// contextExecer is implemented by both *sql.DB and *sql.Tx
type contextExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// GrantMetricsExporterConnect grants CONNECT on the given database to the
// cnpg_metrics_exporter role, which must already exist
func GrantMetricsExporterConnect(ctx context.Context, db contextExecer, database string) error {
	if _, err := db.ExecContext(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s",
		pgx.Identifier{database}.Sanitize(), apiv1.MetricsExporterUserName)); err != nil {
		return fmt.Errorf("while granting CONNECT on database %q to %s: %w",
			database, apiv1.MetricsExporterUserName, err)
	}

	return nil
}

// RestrictDatabaseConnect revokes CONNECT on the given database from PUBLIC
func RestrictDatabaseConnect(ctx context.Context, db contextExecer, database string) error {
	if _, err := db.ExecContext(ctx, fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM PUBLIC",
		pgx.Identifier{database}.Sanitize())); err != nil {
		return fmt.Errorf("while revoking CONNECT on database %q from PUBLIC: %w", database, err)
	}

	return nil
}

// SetupNewDatabase applies the default CONNECT privileges to a database that
// has just been created: it revokes them from PUBLIC and grants them to the
// cnpg_metrics_exporter role, which must already exist. The two changes are
// applied in a transaction, so that a failure leaves the database as it was.
func SetupNewDatabase(ctx context.Context, db *sql.DB, database string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("while starting a transaction to restrict database %q: %w", database, err)
	}
	defer func() {
		// This is a no-op when the transaction is committed
		_ = tx.Rollback()
	}()

	if err := RestrictDatabaseConnect(ctx, tx, database); err != nil {
		return err
	}
	if err := GrantMetricsExporterConnect(ctx, tx, database); err != nil {
		return err
	}

	return tx.Commit()
}

// MetricsExporterRoleExists reports whether the cnpg_metrics_exporter role exists
func MetricsExporterRoleExists(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1",
		apiv1.MetricsExporterUserName).Scan(&exists)
	return exists, err
}

// EnsureMetricsExporterRole opens a transaction and creates or repairs the
// cnpg_metrics_exporter role in it
func EnsureMetricsExporterRole(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// This is a no-op when the transaction is committed
		_ = tx.Rollback()
	}()

	if err := SetupMetricsExporterRole(ctx, tx); err != nil {
		return err
	}

	return tx.Commit()
}

// SetupMetricsExporterRole creates or repairs the cnpg_metrics_exporter role
// inside the supplied transaction: ensures it exists, enforces its attributes,
// and grants pg_monitor membership. When the role is created, it is also
// granted CONNECT on the postgres and template1 databases.
func SetupMetricsExporterRole(ctx context.Context, tx *sql.Tx) error {
	var existsRole bool
	row := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1",
		apiv1.MetricsExporterUserName)
	if err := row.Scan(&existsRole); err != nil {
		return err
	}
	if !existsRole {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"CREATE ROLE %s WITH LOGIN PASSWORD NULL", apiv1.MetricsExporterUserName)); err != nil {
			return err
		}

		for _, database := range metricsExporterDefaultDatabases {
			if err := GrantMetricsExporterConnect(ctx, tx, database); err != nil {
				return err
			}
		}
	}

	var hasCorrectAttrs, hasPgMonitor, hasNoPassword bool
	row = tx.QueryRowContext(ctx,
		`SELECT rolinherit AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
		        AND NOT rolreplication AND NOT rolbypassrls AND rolcanlogin,
		        pg_catalog.pg_has_role(rolname, 'pg_monitor', 'member'),
		        rolpassword IS NULL
		 FROM pg_catalog.pg_authid WHERE rolname = $1`,
		apiv1.MetricsExporterUserName)
	if err := row.Scan(&hasCorrectAttrs, &hasPgMonitor, &hasNoPassword); err != nil {
		return err
	}

	if !hasCorrectAttrs {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"ALTER ROLE %s WITH NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS INHERIT LOGIN",
			apiv1.MetricsExporterUserName)); err != nil {
			return err
		}
	}

	if !hasPgMonitor {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"GRANT pg_monitor TO %s", apiv1.MetricsExporterUserName)); err != nil {
			return err
		}
	}

	if !hasNoPassword {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"ALTER ROLE %s PASSWORD NULL", apiv1.MetricsExporterUserName)); err != nil {
			return err
		}
	}

	return nil
}
