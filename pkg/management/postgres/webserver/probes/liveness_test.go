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

package probes

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/watchdog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("livenessExecutor", func() {
	It("reports OK when the lease watchdog is healthy and there's no unresponsive shutdown", func() {
		executor := &livenessExecutor{
			instance:      &postgres.Instance{},
			leaseWatchdog: watchdog.NewLeaseWatchdog(time.Minute),
		}
		w := httptest.NewRecorder()

		executor.IsHealthy(context.Background(), w)

		Expect(w.Code).To(Equal(200))
		Expect(w.Body.String()).To(Equal("OK"))
	})

	It("fails when the lease watchdog reports the loop stuck after acquiring the lease", func() {
		staleWatchdog := watchdog.NewLeaseWatchdog(time.Nanosecond)
		staleWatchdog.MarkAcquired()
		time.Sleep(time.Millisecond)

		executor := &livenessExecutor{
			instance:      &postgres.Instance{},
			leaseWatchdog: staleWatchdog,
		}
		w := httptest.NewRecorder()

		executor.IsHealthy(context.Background(), w)

		Expect(w.Code).To(Equal(500))
		Expect(w.Body.String()).To(ContainSubstring("primary lease loop stuck"))
	})

	It("reports OK when the watchdog is stale but the lease was never acquired", func() {
		// A replica, or a primary still competing for the lease for the first
		// time, never gets past this point: the loop hasn't stalled, it just
		// hasn't had a reason to run yet.
		staleWatchdog := watchdog.NewLeaseWatchdog(time.Nanosecond)
		time.Sleep(time.Millisecond)

		executor := &livenessExecutor{
			instance:      &postgres.Instance{},
			leaseWatchdog: staleWatchdog,
		}
		w := httptest.NewRecorder()

		executor.IsHealthy(context.Background(), w)

		Expect(w.Code).To(Equal(200))
		Expect(w.Body.String()).To(Equal("OK"))
	})

	It("fails when PostgreSQL is unresponsive to an immediate shutdown", func() {
		// A fake pg_ctl ahead of the real one on PATH: "stop" always fails and
		// "status" always reports the server as running, so an immediate
		// shutdown attempt exhausts its timeout without PostgreSQL actually
		// stopping - exactly the condition the liveness probe must catch.
		binDir, err := os.MkdirTemp("", "fake-pg-ctl")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.RemoveAll(binDir) }()

		script := "#!/bin/sh\ncase \"$*\" in\n*status*) exit 0 ;;\n*) exit 1 ;;\nesac\n"
		//nolint:gosec // executable fake pg_ctl needs the execute bit
		Expect(os.WriteFile(filepath.Join(binDir, "pg_ctl"), []byte(script), 0o700)).To(Succeed())

		origPath := os.Getenv("PATH")
		defer func() { _ = os.Setenv("PATH", origPath) }()
		Expect(os.Setenv("PATH", fmt.Sprintf("%s%c%s", binDir, os.PathListSeparator, origPath))).To(Succeed())

		instance := &postgres.Instance{}
		Expect(instance.TryShuttingDownImmediate(context.Background())).To(HaveOccurred())

		executor := &livenessExecutor{
			instance:      instance,
			leaseWatchdog: watchdog.NewLeaseWatchdog(time.Minute),
		}
		w := httptest.NewRecorder()

		executor.IsHealthy(context.Background(), w)

		Expect(w.Code).To(Equal(500))
		Expect(w.Body.String()).To(ContainSubstring("unresponsive to immediate shutdown"))
	})
})
