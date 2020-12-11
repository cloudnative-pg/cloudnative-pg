/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"

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
		return errors.Wrap(err, "Error promoting the PostgreSQL instance")
	}

	timeLimit := time.Now().Add(1 * time.Minute)
	for {
		if time.Now().After(timeLimit) {
			log.Log.Info("The standby.signal file still exists but timeout reached, " +
				"error during PostgreSQL instance promotion")
			return errors.New("standby.signal still existent")
		}

		if status, _ := instance.IsPrimary(); status {
			break
		}

		time.Sleep(1 * time.Second)
	}

	log.Log.Info("The PostgreSQL instance has been promoted successfully")

	return nil
}
