/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// PromoteAndWait promotes this instance, and wait 60 seconds for it to happen
func (instance *Instance) PromoteAndWait() error {
	instance.ShutdownConnections()

	options := []string{
		"-D",
		instance.PgData,
		"-w",
		"promote",
	}

	log.Log.Info("Promoting instance", "pgctl_options", options)

	cmd := exec.Command("pg_ctl", options...) // #nosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("error promoting the PostgreSQL instance: %w", err)
	}

	timeLimit := time.Now().Add(1 * time.Minute)
	for {
		if time.Now().After(timeLimit) {
			log.Log.Info("The standby.signal file still exists but timeout reached, " +
				"error during PostgreSQL instance promotion")
			return fmt.Errorf("standby.signal still existent")
		}

		if status, _ := instance.IsPrimary(); status {
			break
		}

		time.Sleep(1 * time.Second)
	}

	log.Log.Info("Requesting a checkpoint")

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("after having promoted the instance: %v", err)
	}

	err = db.Ping()
	if err != nil {
		return fmt.Errorf("after having promoted the instance: %v", err)
	}

	// For pg_rewind to work we need to issue a checkpoint here
	_, err = db.Exec("CHECKPOINT")
	if err != nil {
		return fmt.Errorf("checkpoint after instance promotion: %v", err)
	}

	log.Log.Info("The PostgreSQL instance has been promoted successfully")

	return nil
}
