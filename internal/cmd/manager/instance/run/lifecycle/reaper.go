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
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/mitchellh/go-ps"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// PostgresOrphansReaper implements the Runnable interface and handles orphaned
// postmaster child processes.
//
// We are running as PID 1 in our container and we may receive SIGCHLD for
// the following reasons:
//
// 1 - a child process we manually executed via `exec.Cmd` (i.e. the postmaster) exited
// 2 - a postmaster worker process terminated after the postmaster itself
//
// The second condition may seem unlikely but it unfortunately happens everytime
// the postmaster ends, even if just for the logging collector subprocess.
//
// As we can see in the postgres codebase (see [1]) the logging collector process
// will ignore any termination signal sent by the postmaster and just wait for
// every upstream process to be gone. This is why it will terminate after the
// postmaster and we'll receive a SIGCHLD.
//
// If we don't collect the logging collector exit code we would get a
// zombie process everytime we restart the postmaster.
//
// Anyway, if we just accept any SIGCHLD signal, we would break the internals
// of `os.exec.Command` preventing the detection of the exit code of the child
// process.
//
// This is why our zombie reaper works only for processes whose
// executable is `postgres`.
//
// [1]: https://github.com/postgres/postgres/blob/REL_14_STABLE/src/backend/postmaster/syslogger.c#L237
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
		if p.PPid() != 1 || p.Executable() != postgres.GetPostgresExecutableName() {
			continue
		}

		pid := p.Pid()
		if pid == postMasterPid {
			continue
		}

		var ws syscall.WaitStatus
		var ru syscall.Rusage
		wpid, werr := syscall.Wait4(pid, &ws, syscall.WNOHANG, &ru)

		if wpid <= 0 {
			// Still running (WNOHANG)
			continue
		}
		if werr != nil {
			if errors.Is(werr, syscall.ECHILD) {
				// No such child; someone else reaped it.
				continue
			}
			contextLogger.Error(werr, "error waiting on orphaned postgres child", "pid", pid)
			continue
		}

		exitCode := ws.ExitStatus()
		contextLogger.Info(
			"reaped orphaned postgres child process",
			"pid", pid,
			"wpid", wpid,
			"exitCode", exitCode,
			"signaled", ws.Signaled(),
			"signal", ws.Signal(),
		)
	}

	return nil
}
