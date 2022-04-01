/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package lifecycle

import (
	"errors"
	"os/exec"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// tryShuttingDownFastImmediate first tries to shut down the instance with mode fast,
// then in case of failure or the given timeout expiration,
// it will issue an immediate shutdown request and wait for it to complete.
// N.B. immediate shutdown can cause data loss.
func tryShuttingDownFastImmediate(timeout int32, instance *postgres.Instance) error {
	log.Info("Requesting fast shutdown of the PostgreSQL instance")
	err := instance.Shutdown(postgres.ShutdownOptions{
		Mode:    postgres.ShutdownModeFast,
		Wait:    true,
		Timeout: &timeout,
	})
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		log.Info("Graceful shutdown failed. Issuing immediate shutdown",
			"exitCode", exitError.ExitCode())
		err = instance.Shutdown(postgres.ShutdownOptions{
			Mode: postgres.ShutdownModeImmediate,
			Wait: true,
		})
	}
	return err
}

// tryShuttingDownSmartFast first tries to shut down the instance with mode smart,
// then in case of failure or the given timeout expiration,
// it will issue a fast shutdown request and wait for it to complete.
func tryShuttingDownSmartFast(timeout int32, instance *postgres.Instance) error {
	log.Info("Requesting smart shutdown of the PostgreSQL instance")
	err := instance.Shutdown(postgres.ShutdownOptions{
		Mode:    postgres.ShutdownModeSmart,
		Wait:    true,
		Timeout: &timeout,
	})
	if err != nil {
		log.Warning("Error while handling the smart shutdown request: requiring fast shutdown",
			"err", err)
		err = instance.Shutdown(postgres.ShutdownOptions{
			Mode: postgres.ShutdownModeFast,
			Wait: true,
		})
	}
	if err != nil {
		log.Error(err, "Error while shutting down the PostgreSQL instance")
	} else {
		log.Info("PostgreSQL instance shut down")
	}
	return err
}
