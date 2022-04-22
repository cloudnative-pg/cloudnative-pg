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
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/mitchellh/go-ps"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// PostgresOrphansReaper implements the Runnable interface and handles orphaned process
type PostgresOrphansReaper struct {
	instance *postgres.Instance
}

// NewPostgresOrphansReaper returns a new PostgresOrphansReaper for an instance
func NewPostgresOrphansReaper(instance *postgres.Instance) *PostgresOrphansReaper {
	return &PostgresOrphansReaper{
		instance: instance,
	}
}

// Start starts the postgres orphaned process reaper
func (z *PostgresOrphansReaper) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGCHLD)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-signalChan:
			err := z.handleSignal(contextLogger)
			if err != nil {
				contextLogger.Error(err, "while handling signal, will continue")
			}
		}
	}
}

func (z *PostgresOrphansReaper) handleSignal(contextLogger log.Logger) error {
	if !z.instance.MightBeUnavailable() {
		return nil
	}
	processes, err := ps.Processes()
	if err != nil {
		return fmt.Errorf("unable to retrieve processes: %w", err)
	}
	pidFile := path.Join(z.instance.PgData, postgres.PostgresqlPidFile)
	_, postMasterPid, _ := z.instance.GetPostmasterPidFromFile(pidFile)
	for _, p := range processes {
		if p.PPid() == 1 && p.Executable() == "postgres" {
			pid := p.Pid()
			if pid == postMasterPid {
				continue
			}
			var ws syscall.WaitStatus
			var ru syscall.Rusage
			wpid, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, &ru)
			if wpid <= 0 || err == nil || err == syscall.ECHILD {
				continue
			}
			contextLogger.Info("reaped orphaned child process", "pid", pid, "err", err, "wpid", wpid)
		}
	}
	return nil
}
