/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package lifecycle

import (
	"errors"
	"os/exec"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metricsserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
)

// fullShutDownSequence runs the full shutdown sequence for the instance
// and everything around it,
// using the provided instanceShutdownFunc to shut down the instance.
// In case of errors in one of the steps, they will be logged,
// but the procedure will continue to completion.
//
// TODO: remove this when the metrics server and the probe server
// will have their own runnable
func fullShutDownSequence(
	instance *postgres.Instance,
	instanceShutdownFunc func(*postgres.Instance) error,
) {
	log.Info("Shutting down the metrics server")
	err := metricsserver.Shutdown()
	if err != nil {
		log.Error(err, "Error while shutting down the metrics server")
	} else {
		log.Info("Metrics server shut down")
	}

	err = instanceShutdownFunc(instance)
	if err != nil {
		log.Error(err, "error shutting down instance, proceeding")
	}

	// We can't shut down the web server before shutting down PostgreSQL.
	// PostgreSQL need it because the wal-archive process need to be able
	// to his job doing the PostgreSQL shut down.
	log.Info("Shutting down web server")
	err = webserver.Shutdown()
	if err != nil {
		log.Error(err, "Error while shutting down the web server")
	} else {
		log.Info("Web server shut down")
	}
}

// tryShuttingDownFastImmediate first tries to shut down the instance with mode fast,
// then in case of failure or the given timeout expiration,
// it will issue an immediate shutdown request and wait for it to complete.
// N.B. immediate shutdown can cause data loss.
func tryShuttingDownFastImmediate(timeout int32) func(instance *postgres.Instance) error {
	return func(instance *postgres.Instance) error {
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
}

// tryShuttingDownSmartFast first tries to shut down the instance with mode smart,
// then in case of failure or the given timeout expiration,
// it will issue a fast shutdown request and wait for it to complete.
func tryShuttingDownSmartFast(timeout int32) func(instance *postgres.Instance) error {
	return func(instance *postgres.Instance) error {
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
}
