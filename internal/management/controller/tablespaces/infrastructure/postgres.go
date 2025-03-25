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

package infrastructure

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// List the tablespaces in the database
// The content exclude pg_default and pg_global database
func List(ctx context.Context, db *sql.DB) ([]Tablespace, error) {
	logger := log.FromContext(ctx).WithName("tbs_reconciler_list")
	logger.Trace("Invoked list")
	wrapErr := func(err error) error { return fmt.Errorf("while listing DB tablespaces: %w", err) }

	rows, err := db.QueryContext(
		ctx,
		`
		SELECT
			pg_tablespace.spcname spcname,
			COALESCE(pg_roles.rolname, '') rolname
		FROM pg_catalog.pg_tablespace
		LEFT JOIN pg_catalog.pg_roles ON pg_tablespace.spcowner = pg_roles.oid
		WHERE spcname NOT LIKE $1
		`,
		postgres.SystemTablespacesPrefix,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Info("Ignorable error while closing pg_catalog.pg_tablespace", "err", closeErr)
		}
	}()

	var tablespaces []Tablespace
	for rows.Next() {
		var tbs Tablespace
		err := rows.Scan(
			&tbs.Name,
			&tbs.Owner,
		)
		if err != nil {
			return nil, wrapErr(err)
		}
		tablespaces = append(tablespaces, tbs)
	}

	if rows.Err() != nil {
		return nil, wrapErr(rows.Err())
	}
	return tablespaces, nil
}

// Create the tablespace in the database, if tablespace is temporary tablespace, need reload configure
func Create(ctx context.Context, db *sql.DB, tbs Tablespace) error {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler_create")
	tablespaceLocation := specs.LocationForTablespace(tbs.Name)

	contextLog.Info("Creating tablespace",
		"tablespace", tbs,
		"tablespaceLocation", tablespaceLocation)
	wrapErr := func(err error) error {
		return fmt.Errorf("while creating tablespace %s: %w", tbs.Name, err)
	}
	var err error
	if _, err = db.ExecContext(
		ctx,
		fmt.Sprintf(
			"CREATE TABLESPACE %s OWNER %s LOCATION '%s'",
			pgx.Identifier{tbs.Name}.Sanitize(),
			pgx.Identifier{tbs.Owner}.Sanitize(),
			tablespaceLocation,
		),
	); err != nil {
		return wrapErr(err)
	}
	return nil
}

// Update the tablespace in the database (change ownership)
func Update(ctx context.Context, db *sql.DB, tbs Tablespace) error {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler_update")
	tablespaceLocation := specs.LocationForTablespace(tbs.Name)

	contextLog.Info("Updating tablespace",
		"tablespace", tbs,
		"tablespaceLocation", tablespaceLocation)
	wrapErr := func(err error) error {
		return fmt.Errorf("while updating tablespace %s: %w", tbs.Name, err)
	}
	var err error
	if _, err = db.ExecContext(
		ctx,
		fmt.Sprintf(
			"ALTER TABLESPACE %s OWNER TO %s",
			pgx.Identifier{tbs.Name}.Sanitize(),
			pgx.Identifier{tbs.Owner}.Sanitize(),
		),
	); err != nil {
		return wrapErr(err)
	}
	return nil
}
