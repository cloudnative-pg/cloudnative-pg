/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	result := postgres.PostgresqlStatus{
		Pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: instance.PodName}},
	}

	row := superUserDB.QueryRow(
		"SELECT system_identifier FROM pg_control_system()")
	err = row.Scan(&result.SystemID)
	if err != nil {
		return nil, err
	}

	row = superUserDB.QueryRow(
		"SELECT NOT pg_is_in_recovery()")
	err = row.Scan(&result.IsPrimary)
	if err != nil {
		return nil, err
	}

	settingsPendingRestart := 0
	row = superUserDB.QueryRow(
		"SELECT COUNT(*) FROM pg_settings WHERE pending_restart")
	err = row.Scan(&settingsPendingRestart)
	if err != nil {
		return nil, err
	}
	result.PendingRestart = settingsPendingRestart > 0

	err = instance.fillWalStatus(&result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// fillWalStatus extract the current WAL information into the PostgresqlStatus
// structure
func (instance *Instance) fillWalStatus(result *postgres.PostgresqlStatus) error {
	if result.IsPrimary {
		return instance.fillWalStatusPrimary(result)
	}

	return instance.fillWalStatusReplica(result)
}

// fillWalStatusPrimary get WAL information for primary servers
func (instance *Instance) fillWalStatusPrimary(result *postgres.PostgresqlStatus) error {
	var err error

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	row := superUserDB.QueryRow(
		"SELECT pg_current_wal_lsn()")
	return row.Scan(&result.CurrentLsn)
}

// fillWalStatusReplica get WAL information for replica servers
func (instance *Instance) fillWalStatusReplica(result *postgres.PostgresqlStatus) error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	row := superUserDB.QueryRow(
		"SELECT pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn(), pg_is_wal_replay_paused()")
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

// GetWALApplyLag gets the amount of bytes of transaction log info need
// still to be applied
func (instance *Instance) GetWALApplyLag() (int64, error) {
	var result int64

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return -1, err
	}

	row := superUserDB.QueryRow("SELECT pg_last_wal_receive_lsn() - pg_last_wal_replay_lsn()")
	err = row.Scan(&result)
	if err != nil {
		return -1, err
	}

	return result, nil
}
