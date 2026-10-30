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
	"fmt"
	"sync/atomic"
	"time"

	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaseWatchdog reports whether the primary-lease loop is still attempting
// lock calls, whether or not they succeed. A stale heartbeat means the loop is
// wedged, not that renewal fails: a primary that retries against an
// unreachable API server is handled by the step-down monitor.
type LeaseWatchdog struct {
	// lastAttempt keeps the monotonic reading of time.Now, so that a step of
	// the wall clock is never mistaken for silence.
	lastAttempt atomic.Pointer[time.Time]
	acquired    atomic.Bool
	maxSilence  atomic.Int64 // nanoseconds
	inFlight    atomic.Int32 // lock calls currently running
}

// NewLeaseWatchdog creates a watchdog that considers the loop stuck once
// maxSilence has elapsed since the last lock call started or returned, with no
// call still running.
func NewLeaseWatchdog(maxSilence time.Duration) *LeaseWatchdog {
	w := &LeaseWatchdog{}
	w.SetMaxSilence(maxSilence)
	w.Beat()
	return w
}

// SetMaxSilence changes the silence limit. It only needs to cover the pause
// between two lock calls: a call still running counts as activity, because
// how long it may block is the API client's business, not a sign of a wedged
// loop.
func (w *LeaseWatchdog) SetMaxSilence(maxSilence time.Duration) {
	w.maxSilence.Store(int64(maxSilence))
}

// Beat records lock activity: a call starting or returning.
func (w *LeaseWatchdog) Beat() {
	now := time.Now()
	w.lastAttempt.Store(&now)
}

// enter marks a lock call as running and returns the function that ends it.
func (w *LeaseWatchdog) enter() func() {
	w.inFlight.Add(1)
	w.Beat()
	return func() {
		w.Beat()
		w.inFlight.Add(-1)
	}
}

// MarkAcquired records that the primary lease has been held by this instance
// at least once. IsHealthy is a no-op until this is called: a replica, or a
// primary still competing for the lease for the first time, never runs the
// renewal loop this watchdog is meant to catch a stall in, and must not be
// fenced for it.
func (w *LeaseWatchdog) MarkAcquired() {
	w.acquired.Store(true)
}

// IsHealthy reports whether the loop has attempted work recently enough.
// It always reports healthy until MarkAcquired has been called at least once.
func (w *LeaseWatchdog) IsHealthy() error {
	if !w.acquired.Load() || w.inFlight.Load() > 0 {
		return nil
	}
	limit := time.Duration(w.maxSilence.Load())
	if silence := time.Since(*w.lastAttempt.Load()); silence > limit {
		return fmt.Errorf("primary lease loop has not made any lock call in %s (limit %s)",
			silence.Round(time.Second), limit)
	}
	return nil
}

// WrapLock returns a resourcelock.Interface that beats before and after
// delegating every method call to lock and reports the watchdog healthy while
// a call is running, so an API server that does not answer is never mistaken
// for a wedged loop. Get and Update are the only methods actually invoked at
// runtime by this elector configuration (the cluster controller owns lease
// creation, so Create is never called), but all methods are wrapped for
// interface completeness.
func (w *LeaseWatchdog) WrapLock(lock resourcelock.Interface) resourcelock.Interface {
	return &watchedLock{inner: lock, watchdog: w}
}

type watchedLock struct {
	inner    resourcelock.Interface
	watchdog *LeaseWatchdog
}

func (l *watchedLock) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	defer l.watchdog.enter()()
	return l.inner.Get(ctx)
}

func (l *watchedLock) Create(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	defer l.watchdog.enter()()
	return l.inner.Create(ctx, ler)
}

func (l *watchedLock) Update(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	defer l.watchdog.enter()()
	return l.inner.Update(ctx, ler)
}

func (l *watchedLock) RecordEvent(s string) {
	l.inner.RecordEvent(s)
}

func (l *watchedLock) Identity() string {
	return l.inner.Identity()
}

func (l *watchedLock) Describe() string {
	return l.inner.Describe()
}
