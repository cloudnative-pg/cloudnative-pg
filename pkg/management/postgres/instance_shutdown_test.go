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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("shutdown attempt timeout arithmetic", func() {
	It("falls back to pg_ctl's own 60 second default when no timeout was requested", func() {
		Expect(shutdownAttemptTimeoutSeconds(nil)).To(Equal(int32(60)))
	})

	It("uses the caller-declared timeout when there is one", func() {
		Expect(shutdownAttemptTimeoutSeconds(ptr.To(int32(15)))).To(Equal(int32(15)))
	})

	It("hands pg_ctl what remains of the budget, not the full value", func() {
		deadline := time.Now().Add(10 * time.Second)
		remaining := remainingShutdownTimeoutSeconds(deadline)
		// Allow for scheduling jitter between computing the deadline above and
		// evaluating it inside remainingShutdownTimeoutSeconds.
		Expect(remaining).To(BeNumerically("~", 10, 1))
	})

	It("floors the remaining budget at 1 second rather than handing pg_ctl a zero or negative timeout", func() {
		alreadyPassed := time.Now().Add(-5 * time.Second)
		Expect(remainingShutdownTimeoutSeconds(alreadyPassed)).To(Equal(int32(1)))
	})
})

var _ = Describe("tryCheckpointBeforeShutdown", func() {
	var instance *Instance
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "checkpoint-before-shutdown")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tempDir) })

		instance = &Instance{PgData: tempDir}
	})

	It("skips the checkpoint entirely for an immediate shutdown, never touching the database", func(ctx SpecContext) {
		// No listener is set up at all: if the immediate-mode skip did not fire,
		// any attempt to dial a superuser connection would either fail fast or,
		// depending on environment, hang. Returning promptly is the assertion.
		done := make(chan struct{})
		go func() {
			instance.tryCheckpointBeforeShutdown(ctx, shutdownModeImmediate, time.Now().Add(time.Minute))
			close(done)
		}()
		Eventually(done).WithTimeout(2 * time.Second).Should(BeClosed())
	})

	It("skips the checkpoint when the instance is not a primary", func(ctx SpecContext) {
		Expect(os.WriteFile(filepath.Join(tempDir, "standby.signal"), nil, 0o600)).To(Succeed())

		done := make(chan struct{})
		go func() {
			instance.tryCheckpointBeforeShutdown(ctx, shutdownModeFast, time.Now().Add(time.Minute))
			close(done)
		}()
		Eventually(done).WithTimeout(2 * time.Second).Should(BeClosed())
	})

	// This is the adversarial case the fix exists for: a primary whose backend
	// accepts the TCP connection (as a frozen postmaster's kernel socket still
	// does) but never completes the protocol handshake, exactly as observed in
	// the reproduction ("Executing CHECKPOINT command before shutdown" followed
	// by nothing at all). Before the fix this blocked forever; the deadline
	// must bound it.
	It("gives up on a CHECKPOINT that does not complete within its deadline, without hanging past it",
		func(ctx SpecContext) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = listener.Close() })

			accepted := make(chan net.Conn, 1)
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr == nil {
					accepted <- conn
				}
			}()

			port := listener.Addr().(*net.TCPAddr).Port
			instance.pool = pool.NewPostgresqlConnectionPool(
				fmt.Sprintf("host=127.0.0.1 port=%d sslmode=disable", port))

			deadline := time.Now().Add(500 * time.Millisecond)
			start := time.Now()

			done := make(chan struct{})
			go func() {
				instance.tryCheckpointBeforeShutdown(ctx, shutdownModeFast, deadline)
				close(done)
			}()

			// The fake server accepted the connection (proving we actually dialed
			// it, not short-circuited some other way) and now just sits there,
			// exactly like a frozen postmaster.
			Eventually(accepted).WithTimeout(2 * time.Second).Should(Receive())

			Eventually(done).WithTimeout(3 * time.Second).Should(BeClosed())
			Expect(time.Since(start)).To(BeNumerically("<", 2*time.Second),
				"the checkpoint must be bounded by its deadline, not by some larger implicit timeout")
		})
})
