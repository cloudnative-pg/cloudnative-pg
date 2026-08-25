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

package leaseobserver

import (
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// Outcome is the result of observing a primary Lease.
type Outcome int

const (
	// NotHeld means the lease is free right now (graceful release, i.e.
	// empty HolderIdentity; or the Lease does not exist yet). Safe to elect
	// immediately, using this pass's freshly-fetched replica status.
	NotHeld Outcome = iota

	// Expired means a holder is still named on the record, but it has not
	// renewed for a full LeaseDurationSeconds as measured by our own wall
	// clock. Safe to elect immediately.
	Expired

	// Held means the lease is actively held (renewed within the last
	// LeaseDurationSeconds, by our own clock) or its liveness has not yet
	// been established (we just started watching it). Do not elect; the
	// caller should requeue.
	Held

	// Unverifiable means the Lease could not be read (any error other than
	// NotFound), or was read but its state could not be classified (e.g. a
	// held lease missing LeaseDurationSeconds — see Observe). Do not elect,
	// requeue, and do not touch previously observed state — this is not
	// evidence of anything.
	Unverifiable
)

// String renders the Outcome as a lowercase word instead of a bare int, so
// Result logs legibly (see Result's json tags and its use in
// observePrimaryLease).
func (o Outcome) String() string {
	switch o {
	case NotHeld:
		return "not_held"
	case Expired:
		return "expired"
	case Held:
		return "held"
	case Unverifiable:
		return "unverifiable"
	default:
		return "unknown"
	}
}

// observation is the last state this tracker locally recorded for one
// cluster's lease.
type observation struct {
	holder string

	// renewTime is the holder's own last-renewal timestamp, exactly as
	// written in the Lease record — i.e. the holder's clock, not ours. It is
	// compared for equality only (did the record change since we last
	// looked), never ordered against our own clock, so clock skew between
	// the controller and whichever instance last wrote the lease cannot
	// affect the Expired decision below.
	renewTime time.Time

	// observedAt is our own wall-clock time when we first saw this exact
	// (holder, renewTime) pair. Expiry is measured as time.Since(observedAt)
	// against the lease's own LeaseDurationSeconds — entirely on our clock.
	observedAt time.Time
}

// Result is the outcome of observing a Lease, together with enough of the
// underlying state for the caller to log something actionable (which holder,
// for how long, against what threshold) without re-deriving it. json tags are
// present so it logs legibly as a single structured field rather than as a
// Go-syntax struct dump.
type Result struct {
	Outcome Outcome `json:"outcome"`

	// Holder is the lease's HolderIdentity. Empty when Outcome is NotHeld.
	Holder string `json:"holder,omitempty"`

	// RenewTime is the holder's own last-renewal timestamp (their clock),
	// zero when Outcome is NotHeld or Unverifiable.
	RenewTime time.Time `json:"renewTime,omitzero"`

	// ObservedFor is how long our own wall clock has seen (Holder, RenewTime)
	// unchanged. Meaningful only when Outcome is Held or Expired.
	ObservedFor time.Duration `json:"observedFor,omitempty"`

	// Duration is the lease's own LeaseDurationSeconds — the threshold
	// ObservedFor is measured against. Meaningful only when Outcome is Held
	// or Expired.
	Duration time.Duration `json:"duration,omitempty"`
}

// Tracker holds one observation per cluster. Safe for concurrent use.
type Tracker struct {
	mu  sync.Mutex
	obs map[types.NamespacedName]observation

	// now is overridable in tests.
	now func() time.Time
}

// NewTracker creates an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{
		obs: make(map[types.NamespacedName]observation),
		now: time.Now,
	}
}

// Observe classifies the current state of a cluster's primary Lease and
// updates this tracker's internal bookkeeping accordingly. lease/getErr are
// the direct result of a client.Get against the Lease object; passing them in
// (rather than having Observe do the Get itself) keeps this type free of any
// client-go/controller-runtime client dependency, mirroring the instance-side
// classifyLeaseAfterRun(checkErr, record, ourIdentity) split between "pure
// classification" and "the Get that feeds it".
func (t *Tracker) Observe(key types.NamespacedName, lease *coordinationv1.Lease, getErr error) Result {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch {
	case apierrors.IsNotFound(getErr):
		delete(t.obs, key)
		return Result{Outcome: NotHeld}
	case getErr != nil:
		return Result{Outcome: Unverifiable}
	}

	holder := ""
	if lease.Spec.HolderIdentity != nil {
		holder = *lease.Spec.HolderIdentity
	}
	if holder == "" {
		delete(t.obs, key)
		return Result{Outcome: NotHeld}
	}

	// LeaseDurationSeconds is +optional at the Kubernetes API level, but
	// every writer of this lease (the instance-manager side, see
	// internal/cmd/manager/instance/run/lease/runnable.go's claim/Release)
	// always sets it: a record naming a holder but no duration doesn't match
	// our own lease protocol, so there's no threshold to measure Expired
	// against and no reasonable default to guess — we can't classify this
	// state, so treat it the same as an unreadable lease rather than
	// assuming a duration nobody actually wrote.
	if lease.Spec.LeaseDurationSeconds == nil {
		return Result{Outcome: Unverifiable, Holder: holder}
	}
	duration := time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second

	renewTime := time.Time{}
	if lease.Spec.RenewTime != nil {
		renewTime = lease.Spec.RenewTime.Time
	}

	now := t.now()
	prev, tracked := t.obs[key]

	// No prior observation, or a different (non-empty) holder, or the same
	// holder but a renewed record: (re)start the observation window. This is
	// also how a change of holder identity resets the clock, per the design.
	if !tracked || prev.holder != holder || !prev.renewTime.Equal(renewTime) {
		t.obs[key] = observation{holder: holder, renewTime: renewTime, observedAt: now}
		return Result{Outcome: Held, Holder: holder, RenewTime: renewTime, ObservedFor: 0, Duration: duration}
	}

	observedFor := now.Sub(prev.observedAt)
	outcome := Held
	if observedFor >= duration {
		outcome = Expired
	}
	return Result{
		Outcome:     outcome,
		Holder:      holder,
		RenewTime:   renewTime,
		ObservedFor: observedFor,
		Duration:    duration,
	}
}

// Forget discards any observation held for a cluster. Call this:
//   - right after the controller commits to a brand-new failover (so the
//     next lease read starts a fresh window rather than reusing a window
//     computed for a previous, unrelated hand-over);
//   - once election actually proceeds on Expired (same reason);
//   - whenever the caller finds no failover/switchover in progress at all
//     (steady state), so a stale window left over from an earlier,
//     already-resolved event doesn't linger;
//   - on cluster deletion, to bound memory growth over the operator's
//     lifetime.
func (t *Tracker) Forget(key types.NamespacedName) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.obs, key)
}
