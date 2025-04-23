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
	"fmt"
	"os/exec"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"

	// this is needed to correctly open the sql connection with the pgx driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ClonePgData clones an existing server, given its connection string,
// to a certain data directory
func ClonePgData(ctx context.Context, connectionString, targetPgData, walDir string) error {
	log.Info("Waiting for server to be available", "connectionString", connectionString)

	db, err := pool.NewDBConnection(connectionString, pool.ConnectionProfilePostgresqlPhysicalReplication)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	err = waitForStreamingConnectionAvailable(ctx, db)
	if err != nil {
		return fmt.Errorf("source server not available: %v", connectionString)
	}

	options := []string{
		"-D", targetPgData,
		"-v",
		"-w",
		"-d", connectionString,
	}

	if walDir != "" {
		options = append(options, "--waldir", walDir)
	}

	pgBaseBackupCmd := exec.Command(pgBaseBackupName, options...) // #nosec
	err = execlog.RunStreaming(pgBaseBackupCmd, pgBaseBackupName)
	if err != nil {
		return fmt.Errorf("error in pg_basebackup, %w", err)
	}

	return nil
}

// Join creates a new instance joined to an existing PostgreSQL cluster
func (info InitInfo) Join(ctx context.Context, cluster *apiv1.Cluster) error {
	primaryConnInfo := buildPrimaryConnInfo(info.ParentNode, info.PodName) + " dbname=postgres connect_timeout=5"

	// We explicitly disable wal_sender_timeout for join-related pg_basebackup executions.
	// A short timeout could not be enough in case the instance is slow to send data,
	// like when the I/O is overloaded.
	primaryConnInfo += " options='-c wal_sender_timeout=0s'"

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	if err := ClonePgData(ctx, primaryConnInfo, info.PgData, info.PgWal); err != nil {
		return err
	}

	slotName := cluster.GetSlotNameFromInstanceName(info.PodName)
	_, err := UpdateReplicaConfiguration(info.PgData, info.GetPrimaryConnInfo(), slotName)
	return err
}
