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
	"errors"
	"fmt"
	"syscall"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
)

const (
	// writePathCheckApplicationName is the application name of the connection
	// used by the write path check
	writePathCheckApplicationName = "cnpg-write-path-check"

	// writePathCheckMessagePrefix is the prefix of the logical message emitted
	// by the write path check
	writePathCheckMessagePrefix = "cnpg-write-path-check"

	// writePathCheckQuery forces a local WAL flush. The synchronous commit is
	// explicitly limited to the local flush: the check has to measure the local
	// write path, not the synchronous standbys.
	writePathCheckQuery = "BEGIN; SET LOCAL synchronous_commit = local; " +
		"SELECT pg_catalog.pg_logical_emit_message(true, '" + writePathCheckMessagePrefix + "', ''); COMMIT;"
)

// WritePathHeartbeat performs a heartbeat of the write path check: it emits a
// transactional logical message, which forces a WAL flush at commit, waiting
// for the local flush only. It uses its own connection, that is kept between
// two calls and dropped on error, so that it never competes with the pool.
//
// The caller measures the duration of the call: a hung heartbeat is a stall,
// while an error is not.
func (instance *Instance) WritePathHeartbeat(ctx context.Context) error {
	if instance.writePathConn == nil || instance.writePathConn.IsClosed() {
		dsn := fmt.Sprintf(
			"host=%s port=%v user=%v sslmode=disable application_name=%v",
			GetSocketDir(),
			GetServerPort(),
			"postgres",
			writePathCheckApplicationName,
		)
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return fmt.Errorf("while connecting for the write path check: %w", err)
		}
		instance.writePathConn = conn
	}

	// Without arguments, pgx uses the simple protocol, which accepts the
	// multiple statements of the query
	if _, err := instance.writePathConn.Exec(ctx, writePathCheckQuery); err != nil {
		_ = instance.writePathConn.Close(context.WithoutCancel(ctx))
		instance.writePathConn = nil
		return fmt.Errorf("while executing the write path heartbeat: %w", err)
	}

	return nil
}

// TerminateWalReceiver sends SIGTERM to the WAL receiver process of this
// PostgreSQL instance. It returns true if the process was signalled.
//
// This ends the replication connection from the replica side, which is what
// lets a failover proceed when the primary is blocked and can't end its
// walsenders: the operator waits for every WAL receiver to be down.
func (instance *Instance) TerminateWalReceiver(ctx context.Context) (bool, error) {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return false, err
	}

	return terminateWalReceiver(ctx, superUserDB, syscall.Kill)
}

// terminateWalReceiver finds the WAL receiver in the passed database and signals
// it with SIGTERM. The kill function is injected for testing.
func terminateWalReceiver(
	ctx context.Context,
	db *sql.DB,
	kill func(pid int, sig syscall.Signal) error,
) (bool, error) {
	var pid *int
	if err := db.QueryRowContext(ctx,
		"SELECT pid FROM pg_catalog.pg_stat_wal_receiver LIMIT 1").Scan(&pid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if pid == nil || *pid <= 0 {
		return false, nil
	}

	log.FromContext(ctx).Info("Terminating the WAL receiver", "pid", *pid)
	if err := kill(*pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, err
	}
	return true, nil
}
