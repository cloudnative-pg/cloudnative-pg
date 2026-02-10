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

package roles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
)

// List the available roles excluding all the roles that start with `pg_`
func List(ctx context.Context, db *sql.DB) ([]DatabaseRole, error) {
	logger := log.FromContext(ctx).WithName("roles_reconciler")
	wrapErr := func(err error) error {
		return fmt.Errorf("while listing DB roles for role reconciler: %w", err)
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, 
       			rolcanlogin, rolreplication, rolconnlimit, rolpassword, rolvaliduntil, rolbypassrls,
				pg_catalog.shobj_description(auth.oid, 'pg_authid') as comment, auth.xmin,
				mem.inroles
		FROM pg_catalog.pg_authid as auth
		LEFT JOIN (
			SELECT pg_catalog.array_agg(pg_catalog.pg_get_userbyid(roleid)) as inroles, member
			FROM pg_catalog.pg_auth_members GROUP BY member
		) mem ON member = oid
		WHERE rolname not like 'pg\_%'`)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Info("Ignorable error while querying pg_catalog.pg_authid", "err", err)
		}
	}()

	var roles []DatabaseRole
	for rows.Next() {
		var comment sql.NullString
		var role DatabaseRole
		var inRoles pq.StringArray
		err := rows.Scan(
			&role.Name,
			&role.Superuser,
			&role.Inherit,
			&role.CreateRole,
			&role.CreateDB,
			&role.Login,
			&role.Replication,
			&role.ConnectionLimit,
			&role.password,
			&role.ValidUntil,
			&role.BypassRLS,
			&comment,
			&role.transactionID,
			&inRoles,
		)
		if err != nil {
			return nil, wrapErr(err)
		}
		if comment.Valid {
			role.Comment = comment.String
		}

		role.InRoles = inRoles

		roles = append(roles, role)
	}

	if rows.Err() != nil {
		return nil, wrapErr(rows.Err())
	}

	return roles, nil
}

func executeRoleStatement(ctx context.Context, db *sql.DB, statement string, silenceErrLog bool) error {
	if !silenceErrLog {
		_, err := db.ExecContext(ctx, statement)
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			errRollback := tx.Rollback()
			if errRollback != nil {
				err = errors.Join(err, errRollback)
			}
		}
	}()
	_, err = tx.ExecContext(ctx, "SET LOCAL log_min_error_statement = 'PANIC'")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, statement)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Update the role
func Update(ctx context.Context, db *sql.DB, role DatabaseRole) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while updating role %s with role reconciler: %w", role.Name, err)
	}
	var query strings.Builder

	query.WriteString(fmt.Sprintf("ALTER ROLE %s ", pgx.Identifier{role.Name}.Sanitize()))
	appendRoleOptions(role, &query)
	contextLog.Debug("Updating role", "role", role.Name, "query", query.String())
	// NOTE: always apply the password update. Since the transaction ID of the role
	// will change no matter what, the next reconciliation cycle we would update the password
	containsPassword := appendPasswordOption(role, &query)
	err := executeRoleStatement(ctx, db, query.String(), containsPassword)
	if err != nil {
		return wrapErr(err)
	}
	return nil
}

// Create the role
// TODO: do we give the role any database-level permissions?
func Create(ctx context.Context, db *sql.DB, role DatabaseRole) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while creating role %s with role reconciler: %w", role.Name, err)
	}

	var query strings.Builder
	query.WriteString(fmt.Sprintf("CREATE ROLE %s ", pgx.Identifier{role.Name}.Sanitize()))
	appendRoleOptions(role, &query)
	appendInRoleOptions(role, &query)
	containsPassword := appendPasswordOption(role, &query)
	contextLog.Debug("Creating", "query", query.String())

	// NOTE: defensively we might think of doing CREATE ... IF EXISTS
	// but at least during development, we want to catch the error
	// Even after, this may be "the kubernetes way"
	if err := executeRoleStatement(ctx, db, query.String(), containsPassword); err != nil {
		return wrapErr(err)
	}

	if len(role.Comment) > 0 {
		query.Reset()
		query.WriteString(fmt.Sprintf("COMMENT ON ROLE %s IS %s",
			pgx.Identifier{role.Name}.Sanitize(), pq.QuoteLiteral(role.Comment)))

		if _, err := db.ExecContext(ctx, query.String()); err != nil {
			return wrapErr(err)
		}
	}

	return nil
}

// Delete the role
func Delete(ctx context.Context, db *sql.DB, role DatabaseRole) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while deleting role %s with role reconciler: %w", role.Name, err)
	}

	query := fmt.Sprintf("DROP ROLE %s", pgx.Identifier{role.Name}.Sanitize())
	contextLog.Debug("Dropping", "query", query)
	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return wrapErr(err)
	}

	return nil
}

// GetLastTransactionID get the last xmin for the role, to help keep track of
// whether the role has been changed in on the Database since last reconciliation
func GetLastTransactionID(ctx context.Context, db *sql.DB, role DatabaseRole) (int64, error) {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while getting last xmin for role %s with role reconciler: %w", role.Name, err)
	}

	var xmin int64
	err := db.QueryRowContext(ctx,
		`SELECT xmin FROM pg_catalog.pg_authid WHERE rolname = $1`,
		role.Name).Scan(&xmin)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, wrapErr(err)
	}
	if err != nil {
		return 0, wrapErr(err)
	}

	return xmin, nil
}

// UpdateComment of the role
func UpdateComment(ctx context.Context, db *sql.DB, role DatabaseRole) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while updating comment for role %s with role reconciler: %w", role.Name, err)
	}

	query := fmt.Sprintf("COMMENT ON ROLE %s IS %s",
		pgx.Identifier{role.Name}.Sanitize(), pq.QuoteLiteral(role.Comment))
	contextLog.Debug("Updating comment", "query", query)
	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return wrapErr(err)
	}

	return nil
}

// UpdateMembership of the role
//
// IMPORTANT: the various REVOKE and GRANT commands that may be required to
// reconcile the role will be done in a single transaction. So, if any one
// of them fails, the role will not get updated
func UpdateMembership(
	ctx context.Context,
	db *sql.DB,
	role DatabaseRole,
	rolesToGrant []string,
	rolesToRevoke []string,
) error {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while updating memberships for role %s with role reconciler: %w", role.Name, err)
	}
	if len(rolesToRevoke)+len(rolesToGrant) == 0 {
		contextLog.Debug("No membership change query to execute for role")
		return nil
	}
	queries := make([]string, 0, len(rolesToRevoke)+len(rolesToGrant))
	for _, r := range rolesToGrant {
		queries = append(queries, fmt.Sprintf(`GRANT %s TO %s`,
			pgx.Identifier{r}.Sanitize(),
			pgx.Identifier{role.Name}.Sanitize()),
		)
	}
	for _, r := range rolesToRevoke {
		queries = append(queries, fmt.Sprintf(`REVOKE %s FROM %s`,
			pgx.Identifier{r}.Sanitize(),
			pgx.Identifier{role.Name}.Sanitize()),
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return wrapErr(err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			contextLog.Error(rollbackErr, "rolling back transaction")
		}
	}()

	for _, sqlQuery := range queries {
		contextLog.Debug("Executing query", "sqlQuery", sqlQuery)
		if _, err := tx.ExecContext(ctx, sqlQuery); err != nil {
			contextLog.Error(err, "executing query", "sqlQuery", sqlQuery, "err", err)
			return wrapErr(err)
		}
	}
	return tx.Commit()
}

// GetParentRoles get the in roles of this role
func GetParentRoles(ctx context.Context, db *sql.DB, role DatabaseRole) ([]string, error) {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Trace("Invoked", "role", role)
	wrapErr := func(err error) error {
		return fmt.Errorf("while getting parents for role %s with role reconciler: %w", role.Name, err)
	}
	query := `SELECT mem.inroles 
		FROM pg_catalog.pg_authid as auth
		LEFT JOIN (
			SELECT pg_catalog.array_agg(pg_catalog.pg_get_userbyid(roleid)) as inroles, member
			FROM pg_catalog.pg_auth_members GROUP BY member
		) mem ON member = oid
		WHERE rolname = $1`
	contextLog.Debug("get parent role", "query", query)
	var parentRoles pq.StringArray
	err := db.QueryRowContext(ctx, query, role.Name).Scan(&parentRoles)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, wrapErr(err)
	}
	if err != nil {
		return nil, wrapErr(err)
	}

	return parentRoles, nil
}

func appendInRoleOptions(role DatabaseRole, query *strings.Builder) {
	if len(role.InRoles) > 0 {
		quotedInRoles := make([]string, len(role.InRoles))

		for i, inRole := range role.InRoles {
			quotedInRoles[i] = pgx.Identifier{inRole}.Sanitize()
		}

		fmt.Fprintf(query, " IN ROLE %s ", strings.Join(quotedInRoles, ","))
	}
}

func appendRoleOptions(role DatabaseRole, query *strings.Builder) {
	if role.BypassRLS {
		query.WriteString(" BYPASSRLS")
	} else {
		query.WriteString(" NOBYPASSRLS")
	}

	if role.CreateDB {
		query.WriteString(" CREATEDB")
	} else {
		query.WriteString(" NOCREATEDB")
	}

	if role.CreateRole {
		query.WriteString(" CREATEROLE")
	} else {
		query.WriteString(" NOCREATEROLE")
	}

	if role.Inherit {
		query.WriteString(" INHERIT")
	} else {
		query.WriteString(" NOINHERIT")
	}

	if role.Login {
		query.WriteString(" LOGIN")
	} else {
		query.WriteString(" NOLOGIN")
	}

	if role.Replication {
		query.WriteString(" REPLICATION")
	} else {
		query.WriteString(" NOREPLICATION")
	}

	if role.Superuser {
		query.WriteString(" SUPERUSER")
	} else {
		query.WriteString(" NOSUPERUSER")
	}

	fmt.Fprintf(query, " CONNECTION LIMIT %d", role.ConnectionLimit)
}

func appendPasswordOption(role DatabaseRole, query *strings.Builder) bool {
	var sendsPassword bool
	switch {
	case role.ignorePassword:
		// Postgres may allow to set the VALID UNTIL of a role independently of
		// having a password or not, so we mimic the behavior by not returning
		// directly
	case !role.password.Valid:
		query.WriteString(" PASSWORD NULL")
	default:
		fmt.Fprintf(query, " PASSWORD %s", pq.QuoteLiteral(role.password.String))
		sendsPassword = true
	}

	if role.ValidUntil.Valid {
		var value string
		if role.ValidUntil.InfinityModifier == pgtype.Finite {
			value = string(pq.FormatTimestamp(role.ValidUntil.Time))
		} else {
			value = role.ValidUntil.InfinityModifier.String()
		}
		fmt.Fprintf(query, " VALID UNTIL %s", pq.QuoteLiteral(value))
	}

	return sendsPassword
}
