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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("shutdown diagnostics", func() {
	It("collects process information from procfs", func() {
		procRoot := GinkgoT().TempDir()
		pidDir := filepath.Join(procRoot, "123")
		Expect(os.Mkdir(pidDir, 0o750)).To(Succeed())

		files := map[string]string{
			"cmdline": "postgres\x00autovacuum worker\x00",
			"comm":    "postgres\n",
			"status":  "Name:\tpostgres\nState:\tT (stopped)\n",
			"wchan":   "do_signal_stop\n",
			"io":      "read_bytes: 0\n",
			"syscall": "operation not permitted\n",
			"stack":   "permission denied\n",
			"sched":   "postgres (123, #threads: 1)\n",
		}
		for name, content := range files {
			Expect(os.WriteFile(filepath.Join(pidDir, name), []byte(content), 0o600)).To(Succeed())
		}

		processes := collectProcDiagnostics(context.Background(), procRoot)

		Expect(processes).To(HaveLen(1))
		Expect(processes[0]).To(HaveField("PID", "123"))
		Expect(processes[0].Files["cmdline"]).To(Equal([]string{"postgres autovacuum worker"}))
		Expect(processes[0].Files["status"]).To(ContainElement("State: T (stopped)"))
		Expect(processes[0].Files["wchan"]).To(Equal([]string{"do_signal_stop"}))
	})
})
