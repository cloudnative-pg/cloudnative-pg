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

import "database/sql"

// ExecuteStatementWithLocalCommit executes a statement with synchronous_commit=local
func ExecuteStatementWithLocalCommit(db *sql.DB, statement string) error {
	return ExecuteWithLocalCommit(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
		return nil
	})
}

// ExecuteWithLocalCommit execute the query with commit ack only when WAL flush to local
// synchronous_commit=local
func ExecuteWithLocalCommit(db *sql.DB, cb func(tx *sql.Tx) error) error {
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
	if _, err := tx.Exec("SET LOCAL synchronous_commit to LOCAL"); err != nil {
		return err
	}

	if err := cb(tx); err != nil {
		return err
	}

	return tx.Commit()
}
