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

func logShutdownDiagnostics(ctx context.Context) {
	diagCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	contextLogger := log.FromContext(ctx)
	contextLogger.Info("PostgreSQL shutdown diagnostics",
		"sample", 1,
		"processes", collectProcDiagnostics(diagCtx, "/proc"))
	time.Sleep(3 * time.Second)
	contextLogger.Info("PostgreSQL shutdown diagnostics",
		"sample", 2,
		"processes", collectProcDiagnostics(diagCtx, "/proc"))
	time.Sleep(3 * time.Second)
	contextLogger.Info("PostgreSQL shutdown diagnostics",
		"sample", 3,
		"processes", collectProcDiagnostics(diagCtx, "/proc"))
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
	data, err := os.ReadFile(fileName)
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
