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
// work, regardless of whether those attempts succeed. A stale heartbeat means
// the loop itself has wedged (e.g. a deadlock); it does NOT mean lease
// renewal is failing. A primary that cannot reach the API server but keeps
// retrying is expected to fail renewal indefinitely while it decides,
// independently, whether it must step down - that is not the condition this
// watchdog detects.
type LeaseWatchdog struct {
	lastAttempt atomic.Int64 // unix nano
	acquired    atomic.Bool
	maxSilence  time.Duration
}

// NewLeaseWatchdog creates a watchdog that considers the loop stuck once
// maxSilence has elapsed since the last recorded lock attempt.
func NewLeaseWatchdog(maxSilence time.Duration) *LeaseWatchdog {
	w := &LeaseWatchdog{maxSilence: maxSilence}
	w.Beat()
	return w
}

// Beat records that a lock operation was just attempted.
func (w *LeaseWatchdog) Beat() {
	w.lastAttempt.Store(time.Now().UnixNano())
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
	if !w.acquired.Load() {
		return nil
	}
	last := time.Unix(0, w.lastAttempt.Load())
	if silence := time.Since(last); silence > w.maxSilence {
		return fmt.Errorf("primary lease loop has not attempted any lock operation in %s (limit %s)",
			silence.Round(time.Second), w.maxSilence)
	}
	return nil
}

// WrapLock returns a resourcelock.Interface that calls Beat() before
// delegating every method call to lock. Get and Update are the only methods
// actually invoked at runtime by this elector configuration (the cluster
// controller owns lease creation, so Create is never called), but all
// methods are wrapped for interface completeness.
func (w *LeaseWatchdog) WrapLock(lock resourcelock.Interface) resourcelock.Interface {
	return &watchedLock{inner: lock, watchdog: w}
}

type watchedLock struct {
	inner    resourcelock.Interface
	watchdog *LeaseWatchdog
}

func (l *watchedLock) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	l.watchdog.Beat()
	return l.inner.Get(ctx)
}

func (l *watchedLock) Create(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	l.watchdog.Beat()
	return l.inner.Create(ctx, ler)
}

func (l *watchedLock) Update(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	l.watchdog.Beat()
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
