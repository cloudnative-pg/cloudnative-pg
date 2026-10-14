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

package lease

import (
	"context"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/utils/ptr"
)

// confirmingLock decorates a resource lock to record when the lease was last
// successfully written with us as the holder. That timestamp is the only
// evidence we have that the lease the replicas can see is still fresh, and it
// is what the step-down monitor reads.
//
// Only writes stamp it. A successful read proves the record still names us, not
// that it is recent: that is why tryTakeOver rewrites a record that already
// names us instead of trusting the read. If reads stamped it too, a state where
// reads succeed and writes do not (separate priority-and-fairness limits for mutating
// requests, an admission policy on leases, a role granting get but not update)
// would reset the clock on every poll while the RenewTime the replicas observe
// stays frozen, which is precisely the situation the monitor exists to catch.
type confirmingLock struct {
	resourcelock.Interface

	// lastConfirmed holds the time, with its monotonic reading, taken just
	// before the last write that named us as the holder started. Nil means no
	// write has been confirmed yet.
	lastConfirmed *atomic.Pointer[time.Time]
}

// Create implements resourcelock.Interface.
func (l *confirmingLock) Create(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	started := time.Now()
	return l.stamp(ler, started, l.Interface.Create(ctx, ler))
}

// Update implements resourcelock.Interface.
func (l *confirmingLock) Update(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	started := time.Now()
	return l.stamp(ler, started, l.Interface.Update(ctx, ler))
}

// stamp advances the clock for a successful write whose record names us as the
// holder. The holder is read from the record we asked to write and not from the
// lock's own identity, because both Release and client-go's release() write a
// record with an empty holder: stamping those would be stamping the hand-over
// that gives the lease away.
//
// The stamp is the time the write started, not the time it returned. The API
// server can commit the write at any point during the call, and a replica
// starts timing the lease from the moment it sees that commit. A slow response
// stamped on arrival would make us believe the lease is fresher than the
// replicas do, and delay the step-down by as long as the response took.
//
// The time.Time is stored as is, so that time.Since measures the age on the
// monotonic clock: a backwards wall-clock step (NTP, a VM resume) would
// otherwise delay the step-down by the size of the step. The replicas time
// their take-over wait on the monotonic clock as well.
func (l *confirmingLock) stamp(ler resourcelock.LeaderElectionRecord, started time.Time, err error) error {
	if err == nil && ler.HolderIdentity == l.Identity() {
		l.lastConfirmed.Store(&started)
	}
	return err
}

// monitorLeaseAge fences this primary once the lease has gone stale and the
// peers agree it should step down. It runs until ctx is cancelled, which the
// lifecycle manager does once PostgreSQL has stopped and the process is about
// to exit, or until it has to stop PostgreSQL itself.
//
// The trigger is the age of the last confirmed lease write rather than a
// failure surfacing in the election loop, because that loop is not guaranteed
// to surface one. client-go's acquire() polls tryAcquireOrRenew with no overall
// deadline, so a process that cannot complete a single round trip stays inside
// it for as long as the outage lasts and le.Run never returns. Keying on the
// age covers both that case and the ordinary one where renewal fails and
// preAcquire takes over the retries.
//
// The stop lands before a replica can claim the lease only if RenewDeadline +
// RetryPeriod + the peer probe timeout is below LeaseDuration, which the
// admission webhook checks. A probe that outlasts the margin delays the stop
// past it, and the hard stop cannot make up for that. The stop comes at an age
// in [LeaseDuration - RetryPeriod, LeaseDuration), so its margin to the earliest
// claim by a replica depends on the tick phase and can be small.
func (r *Runnable) monitorLeaseAge(ctx context.Context) {
	contextLogger := log.FromContext(ctx).WithName("primary-lease")

	// Seed the clock, as a guard: every way into the held state ends in a lease
	// write that stamps it (claim, or renew when the record is already ours), so
	// the stamp is normally set already, and that stamp is the more accurate one
	// and is kept. A zero stamp would otherwise give an enormous age on the very
	// first tick.
	r.lastConfirmed.CompareAndSwap(nil, ptr.To(time.Now()))

	ticker := time.NewTicker(r.config.RetryPeriod)
	defer ticker.Stop()

	// The verdict is remembered, the delivery is retried. Re-deriving the
	// verdict on every tick would re-read the CA from disk and rebuild the
	// certificate pool, dialer and transport to reach a conclusion that cannot
	// reverse, and the wait for the consumer can last minutes.
	stepDownDecided := false
	shutdownRequested := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		age := time.Since(*r.lastConfirmed.Load())
		if !stepDownDecided {
			if age <= r.config.RenewDeadline {
				continue
			}

			stepDown, reason := r.checkStepDown(r.instance.GetClusterOrDefault(), r.instance.GetPodName())
			switch {
			case stepDown:
				contextLogger.Warning("Primary should step down, requesting an immediate shutdown",
					"leaseAge", age, "reason", reason)
				stepDownDecided = true
			case reason != nil:
				contextLogger.Warning("Failed to check primary step-down condition, retrying",
					"leaseAge", age, "error", reason)
				continue
			default:
				contextLogger.Warning(
					"Not stepping down: the other instances are reachable and agree this is the primary, retrying",
					"leaseAge", age)
				continue
			}
		}

		// A declined send means the lifecycle manager is busy running an earlier
		// command, not that it will never take this one, so retry on the next
		// tick. Sending unconditionally would park this goroutine for the whole
		// duration of that command, and a second unconditional send would park
		// it for good: handling the shutdown leaves the consumer's loop running,
		// but the postmaster death that follows takes it out of that loop
		// altogether unless MightBeUnavailable is set (lifecycle.go, the
		// postMasterErrChan branch), so there may be no second receive to meet.
		//
		// Delivery is not the end: the lifecycle manager returning is, and that
		// cancels ctx. If the instance manager is still running at the last tick
		// before a replica may claim the lease, the request was never taken or
		// pg_ctl failed, so stop PostgreSQL here and exit.
		if !shutdownRequested {
			shutdownRequested = r.instance.TryRequestImmediateShutdown()
			if shutdownRequested {
				contextLogger.Info("Immediate shutdown requested, waiting for PostgreSQL to stop")
			} else {
				contextLogger.Warning(
					"The lifecycle manager is busy, could not request the shutdown yet, retrying")
			}
		}
		if age+r.config.RetryPeriod >= r.config.LeaseDuration {
			contextLogger.Error(nil, "Primary lease about to expire with PostgreSQL still running, forcing a stop",
				"leaseAge", age, "shutdownRequested", shutdownRequested)
			r.hardStop()
			return
		}
	}
}

// primaryLeaseLostExitCode is the exit code of the instance manager when it
// fences itself because the primary lease is about to expire. It lets the
// kubelet's last state tell this exit apart from a crash.
const primaryLeaseLostExitCode = 5

// killPostgresAndExit is the last resort fence. The instance manager is PID 1
// of its container, so exiting makes the kernel SIGKILL whatever is left in the
// PID namespace: that exit is the fence. The SIGQUIT to the postmaster, which
// forwards it to its children, only matters when the pod shares its process
// namespace. No SIGKILL follows it: the postmaster could die before forwarding.
// The PID is the one the lifecycle manager holds, so nothing is read from disk
// before the exit.
func (r *Runnable) killPostgresAndExit() {
	if pid := r.instance.GetPostmasterPID(); pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGQUIT)
	}
	os.Exit(primaryLeaseLostExitCode)
}
