/*
Copyright The CloudNativePG Contributors

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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
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
	contextLog := log.FromContext(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// Ensure that at the end of this runnable the instance
	// manager will shut down
	defer i.globalCancel()

	// Every cycle correspond to the lifespan of a postmaster process
	for {
		log.Debug("starting the postgres loop")
		// Start the postmaster. The postMasterErrChan channel
		// will contain any error returned by the process.
		postMasterErrChan := i.runPostgresAndWait(ctx)

	signalLoop:
		for {
			log.Debug("starting signal loop")
			select {
			case err := <-postMasterErrChan:
				// We didn't request postmaster to shut down or to restart, nevertheless
				// the postmaster is terminated. This can happen in the following
				// conditions:
				//
				// 1 - postmaster has crashed
				// 2 - a postmaster child has crashed, and postmaster decided to fly away
				//
				// In this case we want to terminate the instance manager and let the Kubelet
				// restart the Pod.
				if err != nil {
					var exitError *exec.ExitError
					if !errors.As(err, &exitError) {
						contextLog.Error(err, "Error waiting on the PostgreSQL process")
					} else {
						contextLog.Error(exitError, "PostgreSQL process exited with errors")
					}
				}
				if !i.instance.MightBeUnavailable() {
					return err
				}
			case <-ctx.Done():
				// The controller manager asked us to terminate our operations.
				// We shut down PostgreSQL and terminate using the maximum available
				// stop delay. We are doing that because we are not going to receive
				// a SIGKILL by the Kubelet, which is not informed about what's
				// happening.
				if i.instance.InstanceManagerIsUpgrading.Load() {
					log.Info("Context has been cancelled, but an instance manager online upgrade is in progress, " +
						"will just exit")
					return nil
				}
				log.Info("Context has been cancelled, shutting down and exiting")
				if err := tryShuttingDownSmartFast(i.instance.MaxStopDelay, i.instance); err != nil {
					log.Error(err, "error shutting down instance, proceeding")
				}
				return nil

			case sig := <-signals:
				// The kubelet is asking us to terminate by sending a signal
				// to our process. In this case we terminate as fast as we can,
				// otherwise we'll receive a SIGKILL by the Kubelet, possibly
				// resulting in a data corruption.
				smartShutdownTimeout := i.instance.GetSmartShutdownTimeout()
				log.Info("Received termination signal", "signal", sig, "smartShutdownTimeout", smartShutdownTimeout)
				if err := tryShuttingDownSmartFast(smartShutdownTimeout, i.instance); err != nil {
					log.Error(err, "error while shutting down instance, proceeding")
				}
				return nil

			case req := <-i.instance.GetInstanceCommandChan():
				// We received a command issued by the reconciliation loop of
				// the instance manager.
				log.Info("Received request for postgres", "req", req)

				// We execute the requested operation
				restartNeeded, err := i.handleInstanceCommandRequests(req)
				if err != nil {
					log.Error(err, "while handling instance command request")
				}
				if restartNeeded {
					log.Info("Restarting the instance")
					break signalLoop
				}
			}
			log.Debug("exiting signal loop")
		}
		log.Debug("exiting the postgres loop")
		// Here the postmaster is terminated. We need to start a new postmaster
		// process
	}
}

// handleInstanceCommandRequests execute a command requested by the reconciliation
// loop.
func (i *PostgresLifecycle) handleInstanceCommandRequests(
	req postgres.InstanceCommand,
) (restartNeeded bool, err error) {
	if i.instance.IsFenced() {
		switch req {
		case postgres.FenceOff:
			log.Info("Fence lifting request received, will proceed with restarting the instance if needed")
			i.instance.SetFencing(false)
			return true, nil
		default:
			log.Warning("Received request while fencing, ignored", "req", req)
			return false, nil
		}
	}
	switch req {
	case postgres.FenceOn:
		log.Info("Fencing request received, will proceed shutting down the instance")
		i.instance.SetFencing(true)
		err := tryShuttingDownSmartFast(i.instance.MaxStopDelay, i.instance)
		if err != nil {
			err = fmt.Errorf("while shutting down the instance to fence it: %w", err)
		}
		return false, err
	case postgres.RestartSmartFast:
		return true, tryShuttingDownSmartFast(i.instance.MaxStopDelay, i.instance)
	case postgres.ShutDownFastImmediate:
		if err := tryShuttingDownFastImmediate(i.instance.MaxSwitchoverDelay, i.instance); err != nil {
			log.Error(err, "error shutting down instance, proceeding")
		}
		return false, nil
	default:
		return false, fmt.Errorf("unrecognized request: %s", req)
	}
}
