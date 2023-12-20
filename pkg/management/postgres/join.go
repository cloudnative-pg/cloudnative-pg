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

package postgres

import (
	"fmt"
	"os/exec"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"

	// this is needed to correctly open the sql connection with the pgx driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ClonePgData clones an existing server, given its connection string,
// to a certain data directory
func ClonePgData(connectionString, targetPgData, walDir string) error {
	log.Info("Waiting for server to be available", "connectionString", connectionString)

	db, err := pool.NewDBConnection(connectionString, pool.ConnectionProfilePostgresqlPhysicalReplication)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	err = waitForStreamingConnectionAvailable(db)
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
func (info InitInfo) Join(cluster *apiv1.Cluster) error {
	primaryConnInfo := buildPrimaryConnInfo(info.ParentNode, info.PodName) + " dbname=postgres connect_timeout=5"

	pgVersion, err := cluster.GetPostgresqlVersion()
	if err != nil {
		log.Warning(
			"Error while parsing PostgreSQL server version to define connection options, defaulting to PostgreSQL 11",
			"imageName", cluster.GetImageName(),
			"err", err)
	} else if pgVersion >= 120000 {
		// We explicitly disable wal_sender_timeout for join-related pg_basebackup executions.
		// A short timeout could not be enough in case the instance is slow to send data,
		// like when the I/O is overloaded.
		primaryConnInfo += " options='-c wal_sender_timeout=0s'"
	}

	coredumpFilter := cluster.GetCoredumpFilter()
	if err := system.SetCoredumpFilter(coredumpFilter); err != nil {
		return err
	}

	if err = ClonePgData(primaryConnInfo, info.PgData, info.PgWal); err != nil {
		return err
	}

	slotName := cluster.GetSlotNameFromInstanceName(info.PodName)
	_, err = UpdateReplicaConfiguration(info.PgData, info.GetPrimaryConnInfo(), slotName)
	return err
}
