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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// SetupMetricsExporterRole creates or repairs the cnpg_metrics_exporter role
// inside the supplied transaction: ensures it exists, enforces its attributes,
// and grants pg_monitor membership.
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
