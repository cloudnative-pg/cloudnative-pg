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

package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"k8s.io/utils/strings/slices"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// PostgresTablespaceManager is a TablespaceManager for a database instance
type PostgresTablespaceManager struct {
	superUserDB *sql.DB
}

// NewPostgresTablespaceManager returns an implementation of TablespaceManager for postgres
func NewPostgresTablespaceManager(superDB *sql.DB) PostgresTablespaceManager {
	return PostgresTablespaceManager{
		superUserDB: superDB,
	}
}

const sysVarTmpTbs = "TEMP_TABLESPACES"

// List the tablespaces in the database
// The content exclude pg_default and pg_global database
func (tbsMgr PostgresTablespaceManager) List(ctx context.Context) ([]Tablespace, error) {
	logger := log.FromContext(ctx).WithName("tbs_reconciler")
	logger.Trace("Invoked list")
	wrapErr := func(err error) error { return fmt.Errorf("while listing DB tablespaces: %w", err) }

	rows, err := tbsMgr.superUserDB.QueryContext(
		ctx,
		`SELECT spcname, 
       	CASE WHEN spcname=ANY(regexp_split_to_array(current_setting('TEMP_TABLESPACES'),E'\\s*,\\s*')) 
           THEN true ELSE false END AS temp 
		FROM pg_tablespace  
		WHERE spcname NOT IN ('pg_default','pg_global')`)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Info("Ignorable error while querying pg_catalog.pg_tablespace", "err", err)
		}
	}()

	var tablespaces []Tablespace
	for rows.Next() {
		var tbs Tablespace
		err := rows.Scan(
			&tbs.Name,
			&tbs.Temporary,
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

// Update the tablespace in the database
// we only allow the tablespace update the temporary attribute for tablespace right now
func (tbsMgr PostgresTablespaceManager) Update(ctx context.Context, tbs Tablespace) error {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")
	contextLog.Trace("Invoked Update", "tbs", tbs)
	wrapErr := func(err error) error {
		return fmt.Errorf("while update tablespace %s: %w", tbs.Name, err)
	}

	var err error
	var tempTbs []string
	var needUpdate bool
	if tempTbs, err = tbsMgr.getCurrentTemporaryTablespaces(ctx); err != nil {
		return wrapErr(err)
	}
	idx := slices.Index(tempTbs, tbs.Name)

	if tbs.Temporary && idx < 0 {
		tempTbs = append(tempTbs, tbs.Name)
		needUpdate = true
	}

	if !tbs.Temporary && idx >= 0 {
		tempTbs = append(tempTbs[:idx], tempTbs[idx+1:]...)
		needUpdate = true
	}

	if needUpdate {
		contextLog.Debug("Update tablespace to temporary", "tbs", tbs.Name, "temporary", strings.Join(tempTbs, ","))
		if _, err = tbsMgr.superUserDB.ExecContext(ctx, fmt.Sprintf("ALTER SYSTEM SET %s = %s",
			sysVarTmpTbs,
			strings.Join(tempTbs, ","),
		)); err != nil {
			return wrapErr(err)
		}
		if _, err = tbsMgr.superUserDB.ExecContext(ctx, "SELECT pg_reload_conf()"); err != nil {
			return wrapErr(err)
		}
	}

	return nil
}

// Create the tablespace in the database, if tablespace is temporary tablespace, need reload configure
func (tbsMgr PostgresTablespaceManager) Create(ctx context.Context, tbs Tablespace) error {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")
	contextLog.Trace("Invoked Create", "tbs", tbs)
	wrapErr := func(err error) error {
		return fmt.Errorf("while creating tablespace %s: %w", tbs.Name, err)
	}
	var err error
	if _, err = tbsMgr.superUserDB.ExecContext(ctx, fmt.Sprintf("CREATE TABLESPACE %s LOCATION '%s'",
		pgx.Identifier{tbs.Name}.Sanitize(),
		specs.LocationForTablespace(tbs.Name),
	)); err != nil {
		return wrapErr(err)
	}

	var tempTbs []string
	var needUpdate bool
	if tbs.Temporary {
		if tempTbs, err = tbsMgr.getCurrentTemporaryTablespaces(ctx); err != nil {
			return wrapErr(err)
		}
		idx := slices.Index(tempTbs, tbs.Name)
		if idx < 0 {
			tempTbs = append(tempTbs, tbs.Name)
			needUpdate = true
		}
	}

	if needUpdate {
		contextLog.Debug("Set tablespace to template", "tbs", tbs.Name, "temporary value", strings.Join(tempTbs, ","))
		if _, err = tbsMgr.superUserDB.ExecContext(ctx, fmt.Sprintf("ALTER SYSTEM SET %s = %s",
			sysVarTmpTbs,
			strings.Join(tempTbs, ","),
		)); err != nil {
			return wrapErr(err)
		}
		if _, err = tbsMgr.superUserDB.ExecContext(ctx, "SELECT pg_reload_conf()"); err != nil {
			return wrapErr(err)
		}
	}

	return nil
}

// getCurrentTemporaryTablespaces retrieve the current temporary tablespace in slice,
// if there is no temporary tablespace return nil
func (tbsMgr PostgresTablespaceManager) getCurrentTemporaryTablespaces(ctx context.Context) ([]string, error) {
	var tempTbs string
	if err := tbsMgr.superUserDB.QueryRowContext(ctx, fmt.Sprintf("show %s", sysVarTmpTbs)).Scan(&tempTbs); err != nil {
		return nil, err
	}
	if strings.Trim(tempTbs, "") == "" {
		return nil, nil
	}
	return strings.Split(tempTbs, ","), nil
}
