/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"

// IsHealthy check if the instance can really accept connections
func (instance *Instance) IsHealthy() error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	err = superUserDB.Ping()
	if err != nil {
		return err
	}

	return nil
}

// GetStatus Extract the status of this PostgreSQL database
func (instance *Instance) GetStatus() (*postgres.PostgresqlStatus, error) {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	result := postgres.PostgresqlStatus{
		PodName: instance.PodName,
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

	if !result.IsPrimary {
		row = superUserDB.QueryRow(
			"SELECT pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn(), pg_is_wal_replay_paused()")
		err = row.Scan(&result.ReceivedLsn, &result.ReplayLsn, &result.ReplayPaused)
		if err != nil {
			return nil, err
		}

		// Sometimes pg_last_wal_replay_lsn is getting evaluated after
		// pg_last_wal_receive_lsn and this, if other WALs are received,
		// can result in a replay being greater then received. Since
		// we can't force the planner to execute functions in a required
		// order, we fix the result here
		if result.ReceivedLsn.Less(result.ReplayLsn) {
			result.ReceivedLsn = result.ReplayLsn
		}
	}

	return &result, nil
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
