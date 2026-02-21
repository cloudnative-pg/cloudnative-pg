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

package utils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
)

// DisableSuperuserPassword disables the password for the `postgres` user
func DisableSuperuserPassword(db *sql.DB) error {
	var hasPassword bool
	passwordCheck := `SELECT rolpassword IS NOT NULL
		FROM pg_catalog.pg_authid
		WHERE rolname='postgres'`
	err := db.QueryRow(passwordCheck).Scan(&hasPassword)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !hasPassword {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// we don't want to be stuck here if synchronous replicas are still not alive
	// and kicking
	_, err = tx.Exec("ALTER ROLE postgres WITH PASSWORD NULL")
	if err != nil {
		return fmt.Errorf("while running ALTER ROLE %v WITH PASSWORD: %w", "postgres", err)
	}

	return tx.Commit()
}

// ExecWithSuppressedLogging executes a SQL statement within a transaction that
// suppresses PostgreSQL statement logging, preventing passwords from appearing
// in server logs.
func ExecWithSuppressedLogging(ctx context.Context, db *sql.DB, statement string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SET LOCAL log_statement = 'none'"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL log_min_error_statement = 'PANIC'"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, statement); err != nil {
		return err
	}
	return tx.Commit()
}

// SetUserPassword changes the password of a user in the PostgreSQL database
func SetUserPassword(ctx context.Context, username string, password string, db *sql.DB) error {
	return ExecWithSuppressedLogging(ctx, db, fmt.Sprintf("ALTER ROLE %v WITH PASSWORD %v",
		pgx.Identifier{username}.Sanitize(),
		pq.QuoteLiteral(password)))
}
