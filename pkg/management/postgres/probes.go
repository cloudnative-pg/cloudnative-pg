/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import "github.com/2ndquadrant/cloud-native-postgresql/pkg/postgres"

// IsHealthy check if the instance can really accept connections
func (instance *Instance) IsHealthy() error {
	applicationDB, err := instance.GetApplicationDB()
	if err != nil {
		return err
	}

	err = applicationDB.Ping()
	if err != nil {
		return err
	}

	return nil
}

// GetStatus Extract the status of this PostgreSQL database
func (instance *Instance) GetStatus() (*postgres.PostgresqlStatus, error) {
	superUserDb, err := instance.GetSuperuserDB()
	if err != nil {
		return nil, err
	}

	result := postgres.PostgresqlStatus{
		PodName: instance.PodName,
	}

	row := superUserDb.QueryRow(
		"SELECT system_identifier FROM pg_control_system()")
	err = row.Scan(&result.SystemID)
	if err != nil {
		return nil, err
	}

	row = superUserDb.QueryRow(
		"SELECT NOT pg_is_in_recovery()")
	err = row.Scan(&result.IsPrimary)
	if err != nil {
		return nil, err
	}

	if !result.IsPrimary {
		row = superUserDb.QueryRow(
			"SELECT pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn(), pg_is_wal_replay_paused()")
		err = row.Scan(&result.ReceivedLsn, &result.ReplayLsn, &result.ReplayPaused)
		if err != nil {
			return nil, err
		}
	}

	return &result, nil
}

// IsWALReceiverActive check if the WAL receiver process is active by looking
// at the number of records in the `pg_stat_wal_receiver` table
func (instance *Instance) IsWALReceiverActive() (bool, error) {
	var result bool

	superUserDb, err := instance.GetSuperuserDB()
	if err != nil {
		return false, err
	}

	row := superUserDb.QueryRow("SELECT COUNT(*) FROM pg_stat_wal_receiver")
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

	superUserDb, err := instance.GetSuperuserDB()
	if err != nil {
		return -1, err
	}

	row := superUserDb.QueryRow("SELECT pg_last_wal_receive_lsn() - pg_last_wal_replay_lsn()")
	err = row.Scan(&result)
	if err != nil {
		return -1, err
	}

	return result, nil
}
