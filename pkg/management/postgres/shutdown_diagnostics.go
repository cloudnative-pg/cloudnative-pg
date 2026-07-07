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

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

const (
	shutdownDiagnosticsSamples  = 3
	shutdownDiagnosticsInterval = 3 * time.Second
	// shutdownDiagnosticsFirstSampleWait caps how long the shutdown escalation
	// waits for the first sample: reads of /proc/[pid]/{cmdline,status} can
	// block in the kernel and cannot be cancelled from Go.
	shutdownDiagnosticsFirstSampleWait = 3 * time.Second
)

// logShutdownDiagnostics logs a few samples of the state of the processes
// still running in the container. It waits for the first sample, so that the
// processes that resisted the fast shutdown are captured before the immediate
// shutdown starts killing them, while the remaining samples are collected in
// the background: the processes that survive them are the ones worth
// investigating.
func logShutdownDiagnostics(ctx context.Context) {
	contextLogger := log.FromContext(ctx)

	firstSampleLogged := make(chan struct{})
	go func() {
		// The collection is detached from the caller's cancellation on
		// purpose: the samples must survive the manager teardown that may
		// be in progress while this code runs.
		diagCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		for sample := 1; sample <= shutdownDiagnosticsSamples; sample++ {
			processes := collectProcDiagnostics(diagCtx, "/proc")
			for i := range processes {
				// One record per process: a single record with every process
				// would exceed the 16KiB line limit enforced by the container
				// runtimes, and would be split into fragments of invalid JSON.
				contextLogger.Info("PostgreSQL shutdown diagnostics",
					"sample", sample,
					"pid", processes[i].PID,
					"files", processes[i].Files)
			}
			if sample == 1 {
				close(firstSampleLogged)
			}
			if sample == shutdownDiagnosticsSamples {
				return
			}

			select {
			case <-time.After(shutdownDiagnosticsInterval):
			case <-diagCtx.Done():
				return
			}
		}
	}()

	select {
	case <-firstSampleLogged:
	case <-time.After(shutdownDiagnosticsFirstSampleWait):
		contextLogger.Info(
			"Shutdown diagnostics collection is still running, proceeding with the immediate shutdown")
	}
}

type procDiagnostics struct {
	PID   string              `json:"pid"`
	Files map[string][]string `json:"files"`
}

func collectProcDiagnostics(ctx context.Context, procRoot string) []procDiagnostics {
	pids, err := filepath.Glob(filepath.Join(procRoot, "[0-9]*"))
	if err != nil {
		return []procDiagnostics{{
			Files: map[string][]string{
				"proc": {err.Error()},
			},
		}}
	}

	processes := make([]procDiagnostics, 0, len(pids))
	for _, pidDir := range pids {
		if err := ctx.Err(); err != nil {
			return append(processes, procDiagnostics{
				Files: map[string][]string{
					"collection": {err.Error()},
				},
			})
		}

		pid := filepath.Base(pidDir)
		processes = append(processes, procDiagnostics{
			PID: pid,
			Files: map[string][]string{
				"cmdline": readProcLines(filepath.Join(pidDir, "cmdline"), 0, true),
				"comm":    readProcLines(filepath.Join(pidDir, "comm"), 0, false),
				"status":  readProcLines(filepath.Join(pidDir, "status"), 90, false),
				"wchan":   readProcLines(filepath.Join(pidDir, "wchan"), 0, false),
				"io":      readProcLines(filepath.Join(pidDir, "io"), 0, false),
				// stack and syscall are often ptrace/capability gated; log read errors inline.
				"syscall": readProcLines(filepath.Join(pidDir, "syscall"), 0, false),
				"stack":   readProcLines(filepath.Join(pidDir, "stack"), 0, false),
				"sched":   readProcLines(filepath.Join(pidDir, "sched"), 35, false),
			},
		})
	}
	return processes
}

func readProcLines(fileName string, maxLines int, nullSeparated bool) []string {
	data, err := os.ReadFile(filepath.Clean(fileName))
	if err != nil {
		return []string{err.Error()}
	}

	content := string(data)
	if nullSeparated {
		content = strings.ReplaceAll(content, "\x00", " ")
	}
	var result []string
	for lineNumber, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if maxLines > 0 && lineNumber >= maxLines {
			break
		}
		result = append(result, strings.Join(strings.Fields(line), " "))
	}
	return result
}
