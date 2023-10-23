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

package utils

import (
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

	return ExecuteWithLocalCommit(db, func(tx *sql.Tx) error {
		_, err = tx.Exec("ALTER ROLE postgres WITH PASSWORD NULL")
		if err != nil {
			return fmt.Errorf("while running ALTER ROLE %v WITH PASSWORD: %w", "postgres", err)
		}

		return nil
	})
}

// SetUserPassword change the password of a user in the PostgreSQL database
func SetUserPassword(username string, password string, db *sql.DB) error {
	return ExecuteWithLocalCommit(db, func(tx *sql.Tx) error {
		_, err := tx.Exec(fmt.Sprintf("ALTER ROLE %v WITH PASSWORD %v",
			pgx.Identifier{username}.Sanitize(),
			pq.QuoteLiteral(password)))
		if err != nil {
			return fmt.Errorf("while running ALTER ROLE %v WITH PASSWORD: %w", username, err)
		}
		return nil
	})
}
