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
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
)

// DisableSuperuserPassword disables the password for the `postgres` user
func DisableSuperuserPassword(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		// This has no effect if the transaction
		// is committed
		_ = tx.Rollback()
	}()

	// we don't want to be stuck here if synchronous replicas are still not alive
	// and kicking
	_, err = tx.Exec("SET LOCAL synchronous_commit to LOCAL")
	if err != nil {
		return err
	}

	_, err = tx.Exec("ALTER ROLE postgres WITH PASSWORD NULL")
	if err != nil {
		return fmt.Errorf("while running ALTER ROLE %v WITH PASSWORD: %w", "postgres", err)
	}

	return tx.Commit()
}

// SetUserPassword change the password of a user in the PostgreSQL database
func SetUserPassword(username string, password string, db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		// This has no effect if the transaction
		// is committed
		_ = tx.Rollback()
	}()

	// we don't want to be stuck here if synchronous replicas are still not alive
	// and kicking
	_, err = tx.Exec("SET LOCAL synchronous_commit to LOCAL")
	if err != nil {
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("ALTER ROLE %v WITH PASSWORD %v",
		pgx.Identifier{username}.Sanitize(),
		pq.QuoteLiteral(password)))
	if err != nil {
		return fmt.Errorf("while running ALTER ROLE %v WITH PASSWORD: %w", username, err)
	}

	return tx.Commit()
}
