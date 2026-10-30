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

package writepath

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	clocktesting "k8s.io/utils/clock/testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Monitor", func() {
	var (
		fakeClock *clocktesting.FakeClock
		state     *State
		monitor   *Monitor

		beatBlock   chan struct{}
		beatErr     atomic.Value
		beats       atomic.Int32
		stalledCnt  atomic.Int32
		clearedCnt  atomic.Int32
		failingOver atomic.Bool
		enabled     atomic.Bool
	)

	// advance moves the fake clock one second at a time, giving the goroutines
	// of the monitor the time to register their timers
	advance := func(total time.Duration) {
		for elapsed := time.Duration(0); elapsed < total; elapsed += time.Second {
			Eventually(fakeClock.HasWaiters).Should(BeTrue())
			fakeClock.Step(time.Second)
			time.Sleep(5 * time.Millisecond)
		}
	}

	BeforeEach(func() {
		SetDefaultEventuallyTimeout(10 * time.Second)
		fakeClock = clocktesting.NewFakeClock(time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC))
		state = &State{}
		beatBlock = nil
		beatErr = atomic.Value{}
		beats.Store(0)
		stalledCnt.Store(0)
		clearedCnt.Store(0)
		failingOver.Store(false)
		enabled.Store(true)

		var mu sync.Mutex
		monitor = &Monitor{
			State: state,
			Clock: fakeClock,
			Config: func() Config {
				return Config{
					Enabled: enabled.Load(), Period: 10 * time.Second,
					Timeout: 30 * time.Second, ClearAfterSuccesses: 2,
				}
			},
			ShouldRun: func() bool { return true },
			Beat: func(ctx context.Context) error {
				beats.Add(1)
				mu.Lock()
				block := beatBlock
				mu.Unlock()
				if block != nil {
					select {
					case <-block:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if err, ok := beatErr.Load().(error); ok && err != nil {
					return err
				}
				return nil
			},
			FailoverStarted: failingOver.Load,
			OnStalled:       func() { stalledCnt.Add(1) },
			OnCleared:       func() { clearedCnt.Add(1) },
		}

		runCtx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		go func() { _ = monitor.Start(runCtx) }()
	})

	It("does not report a stall while heartbeats complete", func() {
		advance(65 * time.Second)
		Expect(beats.Load()).To(BeNumerically(">=", 5))
		Expect(state.Stalled()).To(BeFalse())
		Expect(stalledCnt.Load()).To(BeZero())
	})

	It("reports a stall after the timeout, starts no new heartbeat meanwhile, and clears it after N successes",
		func() {
			beatBlock = make(chan struct{})
			advance(11 * time.Second) // first heartbeat starts and hangs
			Expect(beats.Load()).To(BeEquivalentTo(1))

			advance(29 * time.Second)
			Expect(state.Stalled()).To(BeFalse())

			advance(3 * time.Second)
			Eventually(state.Stalled).Should(BeTrue())
			Expect(stalledCnt.Load()).To(BeEquivalentTo(1))

			advance(60 * time.Second)
			Expect(beats.Load()).To(BeEquivalentTo(1), "no new heartbeat while the previous one is running")
			Expect(stalledCnt.Load()).To(BeEquivalentTo(1))

			// the disk recovers
			close(beatBlock)
			Eventually(beats.Load).Should(BeEquivalentTo(1))
			advance(30 * time.Second)
			Eventually(state.Stalled).Should(BeFalse())
			Expect(clearedCnt.Load()).To(BeEquivalentTo(1))
		})

	It("does not report a stall on heartbeat errors", func() {
		beatErr.Store(errors.New("no space left on device"))
		advance(120 * time.Second)
		Expect(beats.Load()).To(BeNumerically(">=", 5))
		Expect(state.Stalled()).To(BeFalse())
	})

	It("latches the stall when a failover away from the instance started", func() {
		beatBlock = make(chan struct{})
		advance(45 * time.Second)
		Eventually(state.Stalled).Should(BeTrue())

		failingOver.Store(true)
		advance(2 * time.Second)
		Eventually(state.Latched).Should(BeTrue())

		close(beatBlock)
		advance(60 * time.Second)
		Expect(state.Stalled()).To(BeTrue())
		Expect(clearedCnt.Load()).To(BeZero())
	})

	It("does nothing when the check is disabled", func() {
		enabled.Store(false)
		advance(120 * time.Second)
		Expect(beats.Load()).To(BeZero())
		Expect(state.Stalled()).To(BeFalse())
	})
})
