/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/executablehash"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// IsServerHealthy check if the instance is healthy
func (instance *Instance) IsServerHealthy() error {
	err := instance.PgIsReady()

	// A healthy server can also be actively rejecting connections.
	// That's not a problem: it's only the server starting up or shutting
	// down.
	if errors.Is(err, ErrPgRejectingConnection) {
		return nil
	}

	return err
}

// IsServerReady check if the instance is healthy and can really accept connections
func (instance *Instance) IsServerReady() error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	return superUserDB.Ping()
}

// GetStatus Extract the status of this PostgreSQL database
func (instance *Instance) GetStatus() (*postgres.PostgresqlStatus, error) {
	result := postgres.PostgresqlStatus{
		Pod:                    corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: instance.PodName}},
		InstanceManagerVersion: versions.Version,
	}

	if instance.PgRewindIsRunning {
		// We know that pg_rewind is running, so we exit with the proper status
		// updated, and we can provide that information to the user.
		result.IsPgRewindRunning = true
		return &result, nil
	}
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	row := superUserDB.QueryRow(
		`SELECT
			(pg_control_system()).system_identifier,
			-- True if this is a primary instance
			NOT pg_is_in_recovery() as primary,
			-- True if at least one column requires a restart
			EXISTS(SELECT 1 FROM pg_settings WHERE pending_restart),
			-- The size of database in human readable format
			(SELECT pg_size_pretty(SUM(pg_database_size(oid))) FROM pg_database)`)
	err = row.Scan(&result.SystemID, &result.IsPrimary, &result.PendingRestart, &result.TotalInstanceSize)
	if err != nil {
		return nil, err
	}

	err = instance.fillStatus(&result)
	if err != nil {
		return nil, err
	}

	result.InstanceArch = runtime.GOARCH

	result.ExecutableHash, err = executablehash.Get()
	if err != nil {
		return nil, err
	}

	result.IsInstanceManagerUpgrading = instance.InstanceManagerIsUpgrading

	return &result, nil
}

// fillStatus extract the current instance information into the PostgresqlStatus
// structure
func (instance *Instance) fillStatus(result *postgres.PostgresqlStatus) error {
	var err error

	if result.IsPrimary {
		err = instance.fillStatusFromPrimary(result)
	} else {
		err = instance.fillStatusFromReplica(result)
	}
	if err != nil {
		return err
	}

	return instance.fillWalStatus(result)
}

// fillStatusFromPrimary get information for primary servers (including WAL and replication)
func (instance *Instance) fillStatusFromPrimary(result *postgres.PostgresqlStatus) error {
	var err error

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	row := superUserDB.QueryRow(
		"SELECT " +
			"COALESCE(last_archived_wal, '') , " +
			"COALESCE(last_archived_time,'-infinity'), " +
			"COALESCE(last_failed_wal, ''), " +
			"COALESCE(last_failed_time, '-infinity'), " +
			"COALESCE(last_archived_time,'-infinity') > COALESCE(last_failed_time, '-infinity') AS is_archiving," +
			"pg_walfile_name(pg_current_wal_lsn()) as current_wal, " +
			"pg_current_wal_lsn(), " +
			"(SELECT timeline_id FROM pg_control_checkpoint()) as timeline_id " +
			"FROM pg_catalog.pg_stat_archiver")
	err = row.Scan(&result.LastArchivedWAL,
		&result.LastArchivedWALTime,
		&result.LastFailedWAL,
		&result.LastFailedWALTime,
		&result.IsArchivingWAL,
		&result.CurrentWAL,
		&result.CurrentLsn,
		&result.TimeLineID,
	)

	return err
}

// fillWalStatus retrieves information about the WAL senders processes
// and the on-disk WAL archives status
func (instance *Instance) fillWalStatus(result *postgres.PostgresqlStatus) error {
	var err error

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}
	rows, err := superUserDB.Query(
		`SELECT
			application_name,
			coalesce(state, ''),
			coalesce(sent_lsn::text, ''),
			coalesce(write_lsn::text, ''),
			coalesce(flush_lsn::text, ''),
			coalesce(replay_lsn::text, ''),
			coalesce(write_lag, '0'::interval),
			coalesce(flush_lag, '0'::interval),
			coalesce(replay_lag, '0'::interval),
			coalesce(sync_state, ''),
			coalesce(sync_priority, 0)
		FROM pg_catalog.pg_stat_replication
		WHERE application_name LIKE $1 AND usename = $2`,
		fmt.Sprintf("%s-%%", instance.ClusterName),
		v1.StreamingReplicationUser,
	)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if result.ReplicationInfo == nil {
		result.ReplicationInfo = []postgres.PgStatReplication{}
	}
	for rows.Next() {
		pgr := postgres.PgStatReplication{}
		err := rows.Scan(
			&pgr.ApplicationName,
			&pgr.State,
			&pgr.SentLsn,
			&pgr.WriteLsn,
			&pgr.FlushLsn,
			&pgr.ReplayLsn,
			&pgr.WriteLag,
			&pgr.FlushLag,
			&pgr.ReplayLag,
			&pgr.SyncState,
			&pgr.SyncPriority,
		)
		if err != nil {
			return err
		}
		result.ReplicationInfo = append(result.ReplicationInfo, pgr)
	}

	if err = rows.Err(); err != nil {
		return err
	}

	result.ReadyWALFiles, _, err = GetWALArchiveCounters()
	if err != nil {
		return err
	}

	return nil
}

// fillStatusFromReplica get WAL information for replica servers
func (instance *Instance) fillStatusFromReplica(result *postgres.PostgresqlStatus) error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	// pg_last_wal_receive_lsn may be NULL when using non-streaming
	// replicas
	row := superUserDB.QueryRow(
		"SELECT " +
			"COALESCE(pg_last_wal_receive_lsn()::varchar, ''), " +
			"COALESCE(pg_last_wal_replay_lsn()::varchar, ''), " +
			"pg_is_wal_replay_paused()")
	err = row.Scan(&result.ReceivedLsn, &result.ReplayLsn, &result.ReplayPaused)
	if err != nil {
		return err
	}

	// Sometimes pg_last_wal_replay_lsn is getting evaluated after
	// pg_last_wal_receive_lsn and this, if other WALs are received,
	// can result in a replay being greater then received. Since
	// we can't force the planner to execute functions in a required
	// order, we fix the result here
	if result.ReceivedLsn.Less(result.ReplayLsn) {
		result.ReceivedLsn = result.ReplayLsn
	}

	result.IsWalReceiverActive, err = instance.IsWALReceiverActive()
	if err != nil {
		return err
	}
	return nil
}

// IsWALReceiverActive check if the WAL receiver process is active by looking
// at the number of records in the `pg_stat_wal_receiver` table
func (instance *Instance) IsWALReceiverActive() (bool, error) {
	var result bool

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return false, err
	}

	row := superUserDB.QueryRow("SELECT COUNT(*) FROM pg_stat_wal_receiver")
	err = row.Scan(&result)
	if err != nil {
		return false, err
	}

	return result, nil
}

// GetWALArchiveCounters returns the number of WAL files with status ready,
// and the number of those in status done.
func GetWALArchiveCounters() (ready, done int, err error) {
	files, err := fileutils.GetDirectoryContent(specs.PgWalArchiveStatusPath)
	if err != nil {
		return 0, 0, err
	}

	for _, fileName := range files {
		switch {
		case strings.HasSuffix(fileName, ".ready"):
			ready++
		case strings.HasSuffix(fileName, ".done"):
			done++
		}
	}
	return ready, done, nil
}

// GetReadyWALFiles returns an array containing the list of all the WAL
// files that are marked as ready to be archived.
func GetReadyWALFiles() (fileNames []string, err error) {
	files, err := fileutils.GetDirectoryContent(specs.PgWalArchiveStatusPath)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		fileExtension := filepath.Ext(file)
		if fileExtension == ".ready" {
			fileNames = append(fileNames, strings.TrimSuffix(file, fileExtension))
		}
	}

	return fileNames, nil
}
