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

package logpipe

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// watchCondition closes the returned channel when cond is broadcast, so it
// can be polled with Eventually/Consistently without blocking.
func watchCondition(cond *concurrency.Executed) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		cond.Wait()
	}()
	return done
}

var _ = Describe("non-blocking FIFO open", func() {
	var (
		fileName string
		mu       sync.Mutex
		lines    []string
	)

	BeforeEach(func() {
		fileName = filepath.Join(GinkgoT().TempDir(), "postgres.json")
		lines = nil
	})

	When("no writer is connected to the FIFO", func() {
		It("does not park in the kernel, keeps retrying, and streams lines once a writer shows up", func() {
			p := &LineLogPipe{
				fileName: fileName,
				handler: func(line []byte) {
					mu.Lock()
					defer mu.Unlock()
					lines = append(lines, string(line))
				},
				openFlag:    os.O_RDONLY | nonBlockFlag,
				initialized: concurrency.NewExecuted(),
				exited:      concurrency.NewExecuted(),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			startExited := make(chan struct{})
			go func() {
				defer close(startExited)
				Expect(p.Start(ctx)).To(Succeed())
			}()

			// Without a writer connected, the open(2) call must not block:
			// the reader reaches the "no writer" state (EOF + backoff) and
			// keeps retrying instead of parking an OS thread in the kernel.
			Expect(waitForCondition(p.GetInitializedCondition())).ToNot(HaveOccurred())
			exited := watchCondition(p.GetExitedCondition())
			Consistently(exited, 300*time.Millisecond, 50*time.Millisecond).ShouldNot(BeClosed())

			// Once a writer connects, the lines are streamed to the handler.
			// The write may race the pipe's backoff window (reader fd closed
			// between retries) and hit EPIPE, so retry until the reader is
			// connected.
			writer, err := os.OpenFile(fileName, os.O_WRONLY, 0o600)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() error {
				_, err := writer.Write([]byte("hello from postgres\n"))
				return err
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

			Eventually(func() []string {
				mu.Lock()
				defer mu.Unlock()
				return lines
			}, 5*time.Second, 10*time.Millisecond).Should(ContainElement("hello from postgres"))

			// After the writer disconnects the pipe must not exit on the
			// EOF: it backs off and retries until a new writer shows up.
			Expect(writer.Close()).To(Succeed())
			Consistently(exited, 300*time.Millisecond, 50*time.Millisecond).ShouldNot(BeClosed())

			// And the cancellation unwinds promptly: nothing is parked in
			// the kernel, so no thread is stranded when the bootstrap
			// context goes away.
			cancel()
			Eventually(startExited, 5*time.Second, 10*time.Millisecond).Should(BeClosed())
		})

		It("leaves a goroutine parked in the kernel when the open is blocking instead", func() {
			// Sample the steady-state goroutine count and keep the minimum:
			// transient assertion goroutines would skew a single reading up.
			var base int
			for i := 0; i < 20; i++ {
				if n := runtime.NumGoroutine(); base == 0 || n < base {
					base = n
				}
				time.Sleep(10 * time.Millisecond)
			}

			p := &LineLogPipe{
				fileName:    fileName,
				handler:     func([]byte) {},
				openFlag:    os.O_RDONLY,
				initialized: concurrency.NewExecuted(),
				exited:      concurrency.NewExecuted(),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			startExited := make(chan struct{})
			go func() {
				defer close(startExited)
				_ = p.Start(ctx)
			}()

			Expect(waitForCondition(p.GetInitializedCondition())).ToNot(HaveOccurred())

			// The cancellation unwinds the Start goroutine, but the one
			// parked in the blocking open(2) stays behind, pinning an OS
			// thread until a writer releases the open.
			cancel()
			Eventually(startExited, 5*time.Second, 10*time.Millisecond).Should(BeClosed())
			Consistently(runtime.NumGoroutine, 300*time.Millisecond, 50*time.Millisecond).
				Should(BeNumerically(">=", base+1))

			// Connecting (and releasing) a writer unblocks the parked open.
			writer, err := os.OpenFile(fileName, os.O_WRONLY, 0o600)
			Expect(err).ToNot(HaveOccurred())
			Expect(writer.Close()).To(Succeed())
			Eventually(runtime.NumGoroutine, 5*time.Second, 10*time.Millisecond).
				Should(BeNumerically("<=", base+1))
		})
	})
})
