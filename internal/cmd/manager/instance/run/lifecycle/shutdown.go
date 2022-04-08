/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package lifecycle

import (
	"errors"
	"os/exec"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
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
