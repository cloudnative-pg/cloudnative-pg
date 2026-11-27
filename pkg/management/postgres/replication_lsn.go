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

	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// GetCurrentWALInsertLSN returns the current WAL insert position of the
// primary instance.
//
// The insert position is used (instead of the write position) because it is
// never lower than the commit record of any transaction that has already
// started to commit, even if that record has not been written yet.
func (instance *Instance) GetCurrentWALInsertLSN(ctx context.Context) (string, error) {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return "", err
	}

	return getCurrentWALInsertLSN(ctx, db)
}

// CountStandbysFlushedAtLeast returns how many of the given standbys are
// streaming from this primary and have durably flushed the WAL up to the
// passed LSN (included).
func (instance *Instance) CountStandbysFlushedAtLeast(
	ctx context.Context,
	standbyNames []string,
	lsn string,
) (int, error) {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return 0, err
	}

	return countStandbysFlushedAtLeast(ctx, db, standbyNames, lsn)
}

func getCurrentWALInsertLSN(ctx context.Context, db *sql.DB) (string, error) {
	var lsn string
	if err := db.QueryRowContext(
		ctx,
		"SELECT pg_catalog.pg_current_wal_insert_lsn()::text",
	).Scan(&lsn); err != nil {
		return "", fmt.Errorf("while reading the current WAL insert LSN: %w", err)
	}

	return lsn, nil
}

func countStandbysFlushedAtLeast(
	ctx context.Context,
	db *sql.DB,
	standbyNames []string,
	lsn string,
) (int, error) {
	if len(standbyNames) == 0 {
		return 0, nil
	}

	var count int
	if err := db.QueryRowContext(
		ctx,
		`SELECT count(DISTINCT application_name)
		FROM pg_catalog.pg_stat_replication
		WHERE application_name = ANY($1)
		  AND usename = $2
		  AND state = 'streaming'
		  AND flush_lsn >= $3::pg_catalog.pg_lsn`,
		pq.Array(standbyNames),
		apiv1.StreamingReplicationUser,
		lsn,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("while counting the standbys that flushed LSN %s: %w", lsn, err)
	}

	return count, nil
}

// RequestCheckpoint requests a checkpoint and waits for it to complete.
//
// A requested checkpoint is fast: it completes much sooner than a paced one.
// If the context expires the checkpoint is not interrupted, it keeps running
// on the server.
func (instance *Instance) RequestCheckpoint(ctx context.Context) error {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	return requestCheckpoint(ctx, db)
}

// HasCheckpointStartedAtOrAfter returns true if the last completed checkpoint
// started at or after the passed LSN, that is a position of the WAL insert
// pointer read before requesting a checkpoint. The redo location is used
// because it is where a checkpoint starts: the checkpoint that was already
// running when the request was made started before it, and it doesn't count.
func (instance *Instance) HasCheckpointStartedAtOrAfter(ctx context.Context, lsn string) (bool, error) {
	db, err := instance.GetSuperUserDB()
	if err != nil {
		return false, err
	}

	return hasCheckpointStartedAtOrAfter(ctx, db, lsn)
}

func hasCheckpointStartedAtOrAfter(ctx context.Context, db *sql.DB, lsn string) (bool, error) {
	var result bool
	if err := db.QueryRowContext(
		ctx,
		"SELECT redo_lsn >= $1::pg_catalog.pg_lsn FROM pg_catalog.pg_control_checkpoint()",
		lsn,
	).Scan(&result); err != nil {
		return false, fmt.Errorf("while checking the last checkpoint against LSN %s: %w", lsn, err)
	}

	return result, nil
}

func requestCheckpoint(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return fmt.Errorf("while requesting a checkpoint: %w", err)
	}

	return nil
}
