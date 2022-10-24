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
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/executablehash"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
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
	if !instance.CanCheckReadiness() {
		return fmt.Errorf("instance is not ready yet")
	}
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	return superUserDB.Ping()
}

// GetStatus Extract the status of this PostgreSQL database
func (instance *Instance) GetStatus() (result *postgres.PostgresqlStatus, err error) {
	result = &postgres.PostgresqlStatus{
		Pod:                    corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: instance.PodName}},
		InstanceManagerVersion: versions.Version,
		MightBeUnavailable:     instance.MightBeUnavailable(),
	}

	// this deferred function may override the error returned. Take extra care.
	defer func() {
		if !result.MightBeUnavailable {
			return
		}
		if result.MightBeUnavailable && err == nil {
			return
		}
		// we save the error that we are masking
		result.MightBeUnavailableMaskedError = err.Error()
		// We override the error. We only care about checking if isPrimary is correctly detected
		result.IsPrimary, err = instance.IsPrimary()
		if err != nil {
			return
		}
	}()

	if instance.PgRewindIsRunning {
		// We know that pg_rewind is running, so we exit with the proper status
		// updated, and we can provide that information to the user.
		result.IsPgRewindRunning = true
		return result, nil
	}
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return result, err
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
		return result, err
	}

	if result.PendingRestart {
		err = updateResultForDecrease(instance, superUserDB, result)
		if err != nil {
			return result, err
		}
	}

	err = instance.fillStatus(result)
	if err != nil {
		return result, err
	}

	result.InstanceArch = runtime.GOARCH

	result.ExecutableHash, err = executablehash.Get()
	if err != nil {
		return result, err
	}

	result.IsInstanceManagerUpgrading = instance.InstanceManagerIsUpgrading.Load()

	return result, nil
}

// updateResultForDecrease updates the given postgres.PostgresqlStatus
// in case of pending restart, by checking whether the restart is due to hot standby
// sensible parameters being decreased
func updateResultForDecrease(
	instance *Instance,
	superUserDB *sql.DB,
	result *postgres.PostgresqlStatus,
) error {
	// get all the hot standby sensible parameters being decreased
	decreasedValues, err := instance.GetDecreasedSensibleSettings(superUserDB)
	if err != nil {
		return err
	}

	if len(decreasedValues) == 0 {
		return nil
	}

	// if there is at least one hot standby sensible parameter decreased
	// mark the pending restart as due to a decrease
	result.PendingRestartForDecrease = true
	if !result.IsPrimary {
		// in case of hot standby parameters being decreased,
		// followers need to wait for the new value to be present in the PGDATA before being restarted.
		pgControldataParams, err := GetEnforcedParametersThroughPgControldata(instance.PgData)
		if err != nil {
			return err
		}
		// So, we set PendingRestart according to whether all decreased
		// hot standby sensible parameters have been updated in the PGDATA
		result.PendingRestart = areAllParamsUpdated(decreasedValues, pgControldataParams)
	}
	return nil
}

func areAllParamsUpdated(decreasedValues map[string]string, pgControldataParams map[string]string) bool {
	var readyParams int
	for setting, newValue := range decreasedValues {
		if pgControldataParams[setting] == newValue {
			readyParams++
		}
	}
	return readyParams == len(decreasedValues)
}

// GetDecreasedSensibleSettings tries to get all decreased hot standby sensible parameters from the instance.
// Returns a map containing all the decreased hot standby sensible parameters with their new value.
// See https://www.postgresql.org/docs/current/hot-standby.html#HOT-STANDBY-ADMIN for more details.
func (instance *Instance) GetDecreasedSensibleSettings(superUserDB *sql.DB) (map[string]string, error) {
	// We check whether all parameters with a pending restart from pg_settings
	// have a decreased value reported as not applied from pg_file_settings.
	rows, err := superUserDB.Query(
		`
SELECT pending_settings.name, coalesce(new_setting,default_setting) as new_setting
FROM
   (
	  SELECT name,
			setting as current_setting,
			boot_val as default_setting
	  FROM pg_settings
	  WHERE pending_restart
   ) pending_settings
LEFT OUTER JOIN
	(
		SELECT * FROM
		(
			SELECT name,
				setting as new_setting,
				rank() OVER (PARTITION BY name ORDER BY seqno DESC) as rank,
				applied
			FROM pg_file_settings
		) c
	    WHERE rank = 1 AND not applied
	) file_settings
ON pending_settings.name = file_settings.name
WHERE pending_settings.name IN (
	'max_connections',
	'max_prepared_transactions',
	'max_wal_senders',
	'max_worker_processes',
	'max_locks_per_transaction'
		  )
	AND CAST(coalesce(new_setting,default_setting) AS INTEGER) < CAST(current_setting AS INTEGER)
					`)
	if err != nil {
		return nil, err
	}
	defer func() {
		exitErr := rows.Close()
		if exitErr != nil {
			err = exitErr
		}
	}()

	decreasedSensibleValues := make(map[string]string)
	for rows.Next() {
		var newValue, name string
		if err = rows.Scan(&name, &newValue); err != nil {
			return nil, err
		}
		decreasedSensibleValues[name] = newValue
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return decreasedSensibleValues, nil
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

	if err := instance.fillReplicationSlotsStatus(result); err != nil {
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

func (instance *Instance) fillReplicationSlotsStatus(result *postgres.PostgresqlStatus) error {
	if !result.IsPrimary {
		return nil
	}
	if ver, _ := instance.GetPgVersion(); ver.Major < 13 {
		return nil
	}

	var err error
	var slots postgres.PgReplicationSlotList
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	rows, err := superUserDB.Query(
		`SELECT 
    slot_name,
	coalesce(plugin::text, ''),
	coalesce(slot_type::text, ''),	
	coalesce(datoid::text,''),	
	coalesce(database::text,''),	
	active,
	coalesce(xmin::text, ''),	
	coalesce(catalog_xmin::text, ''),	
	coalesce(restart_lsn::text, ''),
	coalesce(wal_status::text, ''),
	safe_wal_size
    FROM pg_replication_slots`)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	for rows.Next() {
		slot := postgres.PgReplicationSlot{}
		if err := rows.Scan(
			&slot.SlotName,
			&slot.Plugin,
			&slot.SlotType,
			&slot.Datoid,
			&slot.Database,
			&slot.Active,
			&slot.Xmin,
			&slot.CatalogXmin,
			&slot.RestartLsn,
			&slot.WalStatus,
			&slot.SafeWalSize,
		); err != nil {
			return err
		}
		slots = append(slots, slot)
	}

	result.ReplicationSlotsInfo = slots

	if err := rows.Err(); err != nil {
		return err
	}

	return nil
}

// fillWalStatus retrieves information about the WAL senders processes
// and the on-disk WAL archives status
func (instance *Instance) fillWalStatus(result *postgres.PostgresqlStatus) error {
	if !result.IsPrimary {
		return nil
	}
	var err error
	var replicationInfo postgres.PgStatReplicationList

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
		replicationInfo = append(replicationInfo, pgr)
	}
	result.ReplicationInfo = replicationInfo

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
			"(SELECT timeline_id FROM pg_control_checkpoint()), " +
			"COALESCE(pg_last_wal_receive_lsn()::varchar, ''), " +
			"COALESCE(pg_last_wal_replay_lsn()::varchar, ''), " +
			"pg_is_wal_replay_paused()")
	if err := row.Scan(&result.TimeLineID, &result.ReceivedLsn, &result.ReplayLsn, &result.ReplayPaused); err != nil {
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

// PgStatWal is a representation of the pg_stat_wal table
type PgStatWal struct {
	WalRecords     int64
	WalFpi         int64
	WalBytes       int64
	WALBuffersFull int64
	WalWrite       int64
	WalSync        int64
	WalWriteTime   int64
	WalSyncTime    int64
	StatsReset     string
}

// TryGetPgStatWAL retrieves pg_wal_stat on pg version 14 and further
func (instance *Instance) TryGetPgStatWAL() (*PgStatWal, error) {
	version, err := instance.GetPgVersion()
	if err != nil || version.Major < 14 {
		return nil, err
	}

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	var pgWalStat PgStatWal
	row := superUserDB.QueryRow(
		`SELECT
        wal_records,
		wal_fpi,
		wal_bytes,
		wal_buffers_full,
		wal_write,
		wal_sync,
		wal_write_time,
		wal_sync_time,
		stats_reset
	    FROM pg_stat_wal`)
	if err := row.Scan(
		&pgWalStat.WalRecords,
		&pgWalStat.WalFpi,
		&pgWalStat.WalBytes,
		&pgWalStat.WALBuffersFull,
		&pgWalStat.WalWrite,
		&pgWalStat.WalSync,
		&pgWalStat.WalWriteTime,
		&pgWalStat.WalSyncTime,
		&pgWalStat.StatsReset,
	); err != nil {
		return nil, err
	}

	return &pgWalStat, nil
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
