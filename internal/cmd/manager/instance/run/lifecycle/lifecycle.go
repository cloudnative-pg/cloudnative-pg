/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// PostgresLifecycle implements the manager.Runnable interface for a postgres.Instance
type PostgresLifecycle struct {
	instance *postgres.Instance

	globalCtx            context.Context
	globalCancel         context.CancelFunc
	systemInitialization concurrency.MultipleExecuted
}

// NewPostgres creates a new PostgresLifecycle
func NewPostgres(
	ctx context.Context,
	instance *postgres.Instance,
	initialization concurrency.MultipleExecuted,
) *PostgresLifecycle {
	ctx, cancel := context.WithCancel(ctx)
	return &PostgresLifecycle{
		instance:             instance,
		globalCtx:            ctx,
		globalCancel:         cancel,
		systemInitialization: initialization,
	}
}

// GetGlobalContext returns the PostgresLifecycle's context
func (i *PostgresLifecycle) GetGlobalContext() context.Context {
	return i.globalCtx
}

// Start starts running the PostgresLifecycle
// nolint:gocognit
func (i *PostgresLifecycle) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// Ensure that at the end of this runnable the instance
	// manager will shut down
	defer i.globalCancel()

	// Every cycle correspond to the lifespan of a postmaster process
	for {
		contextLogger.Debug("starting the postgres loop")
		// Start the postmaster. The postMasterErrChan channel
		// will contain any error returned by the process.
		postMasterErrChan := i.runPostgresAndWait(ctx)

	signalLoop:
		for {
			pgStopHandler := func(pgExitStatus error) {
				// The postmaster error channel will send an error value, possibly being nil,
				// corresponding to the postmaster exit status.
				// Having done that, it will be closed.
				//
				// Closed channels are always ready for communication and to avoid a spin
				// loop we need to ensure it is never selected again.
				postMasterErrChan = nil

				// We didn't request postmaster to shut down or to restart, nevertheless
				// the postmaster is terminated. This can happen in the following
				// conditions:
				//
				// 1 - postmaster has crashed
				// 2 - a postmaster child has crashed, and postmaster decided to fly away
				//
				// In this case we want to terminate the instance manager and let the Kubelet
				// restart the Pod.
				if pgExitStatus != nil {
					var exitError *exec.ExitError
					if !errors.As(pgExitStatus, &exitError) {
						contextLogger.Error(pgExitStatus, "Error waiting on the PostgreSQL process")
					} else {
						contextLogger.Error(exitError, "PostgreSQL process exited with errors")
					}
				}
			}

			contextLogger.Debug("starting signal loop")
			select {
			case err := <-postMasterErrChan:
				pgStopHandler(err)
				if !i.instance.MightBeUnavailable() {
					return err
				}

			case <-ctx.Done():
				// The controller manager asked us to terminate our operations.
				// We shut down PostgreSQL and terminate using the smart
				// stop delay.
				if i.instance.InstanceManagerIsUpgrading.Load() {
					contextLogger.Info("Context has been cancelled, but an instance manager online upgrade is in progress, " +
						"will just exit")
					return nil
				}
				contextLogger.Info("Context has been cancelled, shutting down and exiting")
				if err := i.instance.TryShuttingDownSmartFast(ctx); err != nil {
					contextLogger.Error(err, "error shutting down instance, proceeding")
				}
				return nil

			case sig := <-signals:
				// The kubelet is asking us to terminate by sending a signal
				// to our process. In this case we terminate as fast as we can,
				// otherwise we'll receive a SIGKILL by the Kubelet, possibly
				// resulting in a data corruption.
				contextLogger.Info("Received termination signal",
					"signal", sig,
					"smartShutdownTimeout", i.instance.GetClusterOrDefault().GetSmartShutdownTimeout(),
				)
				if err := i.instance.TryShuttingDownSmartFast(ctx); err != nil {
					contextLogger.Error(err, "error while shutting down instance, proceeding")
				}
				return nil

			case req := <-i.instance.GetInstanceCommandChan():
				// We received a command issued by the reconciliation loop of
				// the instance manager.
				contextLogger.Info("Received request for postgres", "req", req)

				// We execute the requested operation
				restartNeeded, err := i.instance.HandleInstanceCommandRequests(ctx, req)
				if err != nil {
					contextLogger.Error(err, "while handling instance command request")
				}
				if restartNeeded {
					contextLogger.Info("Instance restart requested, waiting for PostgreSQL to shut down")
					if postMasterErrChan != nil {
						err := <-postMasterErrChan
						pgStopHandler(err)
					}
					contextLogger.Info("PostgreSQL is shut down, starting the postmaster")
					break signalLoop
				}
			}
			contextLogger.Debug("exiting signal loop")
		}
		contextLogger.Debug("exiting the postgres loop")
		// Here the postmaster is terminated. We need to start a new postmaster
		// process
	}
}
