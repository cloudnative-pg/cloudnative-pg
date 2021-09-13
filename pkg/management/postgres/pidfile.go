/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"os"
	"path"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// PostgresqlPidFile is the name of the file which contains
// the PostgreSQL PID file
const PostgresqlPidFile = "postmaster.pid" //wokeignore:rule=master

// CleanupStalePidFile deletes any stale PostgreSQL PID file
//
// The file is created by any instances and left in the PGDATA volume
// in case of unclean termination.
//
// The presence of this file will prevent the starting instance from running.
//
// We assumed we don't need to check whether an actual instance is running
// if the file is present, as the volume is mounted RWO.
// Moreover, a running instance would die when you remove its PID file.
func (instance *Instance) CleanupStalePidFile() error {
	pidFile := path.Join(instance.PgData, PostgresqlPidFile)
	err := os.Remove(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("deleting file %s: %w", pidFile, err)
	}

	log.Info("Deleted stale PostgreSQL pid file from PGDATA directory")

	return nil
}
