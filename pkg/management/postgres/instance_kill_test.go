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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("killPostgresProcessGroup", func() {
	It("SIGKILLs the whole process group of the pid in postmaster.pid, not just that pid", func() {
		tempDir, err := os.MkdirTemp("", "kill-process-group")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tempDir) })

		// A stand-in postmaster with its own process group, so killing the
		// group cannot possibly reach the test binary's own group. A second
		// child is started inside the same group to stand in for a postgres
		// backend that must go down with the postmaster.
		leader := exec.Command("sleep", "120")
		leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		Expect(leader.Start()).To(Succeed())
		leaderDone := waitInBackground(leader)
		DeferCleanup(func() { _ = leader.Process.Kill() })

		leaderPgid, err := syscall.Getpgid(leader.Process.Pid)
		Expect(err).ToNot(HaveOccurred())

		child := exec.Command("sleep", "120")
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: leaderPgid}
		Expect(child.Start()).To(Succeed())
		childDone := waitInBackground(child)
		DeferCleanup(func() { _ = child.Process.Kill() })

		Expect(os.WriteFile(
			filepath.Join(tempDir, PostgresqlPidFile),
			[]byte(strconv.Itoa(leader.Process.Pid)+"\n"),
			0o600,
		)).To(Succeed())

		instance := &Instance{PgData: tempDir}
		Expect(instance.killPostgresProcessGroup(context.Background())).To(Succeed())

		Eventually(leaderDone).WithTimeout(2*time.Second).Should(BeClosed(),
			"the postmaster stand-in must be dead")
		Eventually(childDone).WithTimeout(2*time.Second).Should(BeClosed(),
			"the backend stand-in sharing its group must be dead too")
	})

	It("fails without killing anything when there is no postmaster.pid to read", func() {
		tempDir, err := os.MkdirTemp("", "kill-process-group-missing-pidfile")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tempDir) })

		instance := &Instance{PgData: tempDir}
		Expect(instance.killPostgresProcessGroup(context.Background())).To(HaveOccurred())
	})
})

var _ = Describe("TryShuttingDownFastImmediate escalation", func() {
	var (
		instance *Instance
		tempDir  string
		binDir   string
		logFile  string
		origPath string
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "fast-immediate-escalation")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tempDir) })

		binDir, err = os.MkdirTemp("", "fake-pg-ctl-bin")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(binDir) })

		logFile = filepath.Join(tempDir, "pg_ctl_invocations.log")

		origPath = os.Getenv("PATH")
		Expect(os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)).To(Succeed())
		DeferCleanup(func() { _ = os.Setenv("PATH", origPath) })

		instance = &Instance{PgData: tempDir}
	})

	// installFakePgCtl stands in for the real pg_ctl binary on PATH: it reports
	// "status" as running, then exits with the given code for a "stop" in fast
	// or immediate mode, logging every invocation so the test can inspect which
	// modes were actually tried.
	installFakePgCtl := func(fastExit, immediateExit int) {
		script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for arg in "$@"; do
  if [ "$arg" = "status" ]; then
    exit 0
  fi
done
mode=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-m" ]; then
    mode="$arg"
  fi
  prev="$arg"
done
if [ "$mode" = "fast" ]; then
  exit %d
elif [ "$mode" = "immediate" ]; then
  exit %d
fi
exit 0
`, logFile, fastExit, immediateExit)
		Expect(os.WriteFile(filepath.Join(binDir, "pg_ctl"), []byte(script), 0o700)).To(Succeed()) //nolint:gosec
	}

	startStandinPostmaster := func() (*exec.Cmd, <-chan struct{}) {
		cmd := exec.Command("sleep", "120")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		Expect(cmd.Start()).To(Succeed())
		done := waitInBackground(cmd)
		DeferCleanup(func() { _ = cmd.Process.Kill() })
		Expect(os.WriteFile(
			filepath.Join(tempDir, PostgresqlPidFile),
			[]byte(strconv.Itoa(cmd.Process.Pid)+"\n"),
			0o600,
		)).To(Succeed())
		return cmd, done
	}

	It("kills the process group when both the fast and the immediate pg_ctl attempts fail", func(ctx SpecContext) {
		installFakePgCtl(1, 1)
		_, done := startStandinPostmaster()

		Expect(instance.TryShuttingDownFastImmediate(ctx)).To(Succeed())

		Eventually(done).WithTimeout(2 * time.Second).Should(BeClosed())

		invocations, err := fileutils.ReadFile(logFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(invocations)).To(ContainSubstring("-m fast"))
		Expect(string(invocations)).To(ContainSubstring("-m immediate"))
	})

	It("does not touch the process when the fast attempt already succeeds", func(ctx SpecContext) {
		installFakePgCtl(0, 0)
		_, done := startStandinPostmaster()

		Expect(instance.TryShuttingDownFastImmediate(ctx)).To(Succeed())

		// A real successful pg_ctl stop would have taken PostgreSQL down itself;
		// our stub does not, so the correct signal here is that our code did not
		// escalate to a kill of its own accord.
		Consistently(done).WithTimeout(1*time.Second).ShouldNot(BeClosed(),
			"no kill should have been issued")

		invocations, err := fileutils.ReadFile(logFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(invocations)).To(ContainSubstring("-m fast"))
		Expect(string(invocations)).ToNot(ContainSubstring("-m immediate"))
	})
})

// waitInBackground reaps cmd in a goroutine and returns a channel that is
// closed once the process has exited, so tests can observe termination
// without racing a zombie process that still answers signal 0.
func waitInBackground(cmd *exec.Cmd) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return done
}
