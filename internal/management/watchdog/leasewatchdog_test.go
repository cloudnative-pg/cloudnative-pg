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

package watchdog

import (
	"context"
	"errors"
	"time"

	"k8s.io/client-go/tools/leaderelection/resourcelock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type countingLock struct {
	getCalls    int
	updateCalls int
	getErr      error
}

func (l *countingLock) Get(_ context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	l.getCalls++
	if l.getErr != nil {
		return nil, nil, l.getErr
	}
	return &resourcelock.LeaderElectionRecord{}, nil, nil
}

func (l *countingLock) Create(_ context.Context, _ resourcelock.LeaderElectionRecord) error {
	return nil
}

func (l *countingLock) Update(_ context.Context, _ resourcelock.LeaderElectionRecord) error {
	l.updateCalls++
	return nil
}

func (l *countingLock) RecordEvent(string) {}

func (l *countingLock) Identity() string { return "test-identity" }

func (l *countingLock) Describe() string { return "test-lock" }

var _ = Describe("LeaseWatchdog", func() {
	It("is healthy right after construction", func() {
		w := NewLeaseWatchdog(50 * time.Millisecond)
		Expect(w.IsHealthy()).To(Succeed())
	})

	It("is healthy right after a Beat", func() {
		w := NewLeaseWatchdog(50 * time.Millisecond)
		time.Sleep(10 * time.Millisecond)
		w.Beat()
		Expect(w.IsHealthy()).To(Succeed())
	})

	It("stays healthy once maxSilence elapses without a Beat if the lease was never acquired", func() {
		w := NewLeaseWatchdog(20 * time.Millisecond)
		time.Sleep(30 * time.Millisecond)
		Expect(w.IsHealthy()).To(Succeed())
	})

	It("becomes unhealthy once maxSilence elapses without a Beat after acquiring the lease", func() {
		w := NewLeaseWatchdog(20 * time.Millisecond)
		w.MarkAcquired()
		Eventually(w.IsHealthy).Should(MatchError(ContainSubstring("primary lease loop")))
	})

	Describe("WrapLock", func() {
		It("beats on Get, even when the underlying call fails", func() {
			w := NewLeaseWatchdog(20 * time.Millisecond)
			inner := &countingLock{getErr: errors.New("boom")}
			wrapped := w.WrapLock(inner)

			_, _, err := wrapped.Get(context.Background())
			Expect(err).To(MatchError("boom"))
			Expect(inner.getCalls).To(Equal(1))

			// Even though the attempt failed, the watchdog stays healthy: it
			// measures whether work is being attempted, not whether it succeeds.
			time.Sleep(15 * time.Millisecond)
			Expect(w.IsHealthy()).To(Succeed())
		})

		It("beats on Update", func() {
			w := NewLeaseWatchdog(20 * time.Millisecond)
			inner := &countingLock{}
			wrapped := w.WrapLock(inner)

			Expect(wrapped.Update(context.Background(), resourcelock.LeaderElectionRecord{})).To(Succeed())
			Expect(inner.updateCalls).To(Equal(1))
		})

		It("delegates Identity and Describe without beating", func() {
			w := NewLeaseWatchdog(time.Second)
			inner := &countingLock{}
			wrapped := w.WrapLock(inner)

			Expect(wrapped.Identity()).To(Equal("test-identity"))
			Expect(wrapped.Describe()).To(Equal("test-lock"))
		})
	})
})
