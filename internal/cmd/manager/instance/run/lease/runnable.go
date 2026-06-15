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
	"fmt"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

const (
	// defaultLeaseDuration is how long a lease is valid before it expires.
	defaultLeaseDuration = 15 * time.Second

	// defaultRenewDeadline is how long the holder tries to renew before giving up.
	defaultRenewDeadline = 10 * time.Second

	// defaultRetryPeriod is how frequently a non-holder retries acquiring the lease.
	defaultRetryPeriod = 2 * time.Second

	// defaultReleasedLeaseDuration is the TTL written when explicitly releasing
	// the lease. An empty HolderIdentity already marks the lease as free, but
	// setting the duration to 1 second mirrors the k8s leaderelection.release()
	// behaviour and acts as belt-and-suspenders: even if an acquirer does not
	// treat an empty identity as immediately available, it waits at most one
	// second before the TTL expires and it can take the lease.
	defaultReleasedLeaseDuration = 1 * time.Second
)

// Config holds the tunable timings of the primary lease. They mirror the
// underlying Kubernetes leader-election parameters and are sourced from the
// Cluster's `.spec.primaryLease` stanza (falling back to the defaults above
// when unset).
type Config struct {
	// LeaseDuration is how long a lease is valid before it expires.
	LeaseDuration time.Duration

	// RenewDeadline is how long the holder tries to renew before giving up.
	RenewDeadline time.Duration

	// RetryPeriod is how frequently a non-holder retries acquiring the lease.
	RetryPeriod time.Duration

	// ReleasedLeaseDuration is the TTL written when explicitly releasing the lease.
	ReleasedLeaseDuration time.Duration
}

// defaultConfig returns the built-in primary lease timings.
func defaultConfig() Config {
	return Config{
		LeaseDuration:         defaultLeaseDuration,
		RenewDeadline:         defaultRenewDeadline,
		RetryPeriod:           defaultRetryPeriod,
		ReleasedLeaseDuration: defaultReleasedLeaseDuration,
	}
}

// Runnable manages the primary lease for this instance.
// It starts idle and enters the acquisition/renewal loop only after Acquire is called.
type Runnable struct {
	instance *postgres.Instance
	lock     *resourcelock.LeaseLock

	// config holds the lease timings. It is initialised to the defaults by New
	// and overwritten by the first Acquire call before the runnable activates.
	config Config

	// activateCh is closed by the first Acquire call to unblock Start.
	activateCh   chan struct{}
	activateOnce sync.Once

	// heldCh is closed once the lease has been successfully acquired for the first time.
	heldCh   chan struct{}
	heldOnce sync.Once
}

// New creates a new Runnable.
func New(
	kubeClient kubernetes.Interface,
	instance *postgres.Instance,
) *Runnable {
	return &Runnable{
		instance: instance,
		config:   defaultConfig(),
		lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Namespace: instance.GetNamespaceName(),
				Name:      instance.GetClusterName(),
			},
			Client: kubeClient.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				// Identity is the pod name alone. The operator creates Pods
				// directly and reuses a pod name only after the previous
				// incarnation is confirmed dead, so any process running under
				// this Identity is the legitimate holder.
				Identity: instance.GetPodName(),
			},
		},
		activateCh: make(chan struct{}),
		heldCh:     make(chan struct{}),
	}
}

// Acquire signals the runnable to start competing for the lease using the
// provided configuration, then blocks until the lease is held or ctx is
// cancelled. The configuration is only applied by the first call; subsequent
// calls reuse the timings captured at activation time.
func (r *Runnable) Acquire(ctx context.Context, config Config) error {
	contextLogger := log.FromContext(ctx)

	r.activateOnce.Do(func() {
		// Set the config before closing activateCh: the channel close
		// establishes the happens-before edge that lets Start read it safely.
		r.config = config
		contextLogger.Info("Acquiring primary lease")
		close(r.activateCh)
	})

	select {
	case <-r.heldCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release explicitly releases the lease so that a replica can promote without
// waiting for the TTL to expire. It is a no-op if this pod is not the current
// holder (including if the lease does not exist yet).
// Callers must pass a fresh (non-cancelled) context; the previous run context
// is already cancelled by the time this is called.
// When invoked from the defer in cmd.go, controller-runtime has already waited
// for all runnables — including PostgresLifecycle — to finish. PostgreSQL is
// normally down by then. The exception is a failed in-place instance-manager
// online upgrade: PostgresLifecycle skips the shutdown when
// InstanceManagerIsUpgrading is set, so PostgreSQL may still be running when
// this defer fires. The operator's onlineUpgradeFailOverDelay (30s) covers
// the pod-restart window in that case so no other pod can promote.
func (r *Runnable) Release(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	record, _, err := r.lock.Get(ctx)
	if errors.IsNotFound(err) {
		contextLogger.Debug("Primary lease does not exist, nothing to release")
		return nil
	}
	if err != nil {
		return err
	}
	if record.HolderIdentity != r.lock.LockConfig.Identity {
		contextLogger.Debug("Primary lease held by another identity, nothing to release",
			"holder", record.HolderIdentity)
		return nil
	}

	contextLogger.Info("Releasing primary lease")
	return r.lock.Update(ctx, resourcelock.LeaderElectionRecord{
		LeaseDurationSeconds: int(r.config.ReleasedLeaseDuration / time.Second),
	})
}

// Start implements controller-runtime's Runnable interface.
// The runnable stays idle until Acquire is called, then enters the
// lease acquisition/renewal loop.
func (r *Runnable) Start(ctx context.Context) error {
	select {
	case <-r.activateCh:
		// proceed to active mode
	case <-ctx.Done():
		return nil
	}
	return r.runLeaderElection(ctx)
}

// leaseCheckOutcome is the result of inspecting the primary lease after the
// leader-election loop exits unexpectedly (i.e. without a context cancellation).
type leaseCheckOutcome int

const (
	// leaseMissing means the lease object no longer exists; the cluster
	// controller recreates it on its deletion watch, so we retry.
	leaseMissing leaseCheckOutcome = iota
	// leaseUnverifiable means the lease could not be read (e.g. the API server is
	// unreachable). We have no evidence of preemption, so we retry and rely on
	// the liveness probe to fence us if we are genuinely isolated.
	leaseUnverifiable
	// leasePreempted means the lease is held by a different (or empty) identity:
	// we no longer own it. This is terminal — the primary must stop.
	leasePreempted
	// leaseStillHeld means we are still the holder: a transient renewal blip, retry.
	leaseStillHeld
)

// classifyLeaseAfterRun decides what the primary should do after the
// leader-election loop returns unexpectedly, based on the post-run read of the
// lease. It is intentionally pure so the high-consequence preemption branch —
// the one that stops PostgreSQL — can be unit-tested without driving a real
// election loop.
func classifyLeaseAfterRun(
	checkErr error,
	record *resourcelock.LeaderElectionRecord,
	ourIdentity string,
) leaseCheckOutcome {
	switch {
	case errors.IsNotFound(checkErr):
		return leaseMissing
	case checkErr != nil:
		return leaseUnverifiable
	// A nil checkErr means the lock returned a non-nil record, so the deref below
	// is safe.
	case record.HolderIdentity != ourIdentity:
		return leasePreempted
	default:
		return leaseStillHeld
	}
}

// runLeaderElection runs the leader election loop. It is the core of the
// primary lease mechanism and handles three distinct exit scenarios:
//
//  1. Clean shutdown: ctx is cancelled (e.g. manager shutting down). le.Run
//     returns because it detects ctx.Done(). We return nil — Release() will be
//     called by the deferred shutdown path in cmd.go.
//
//  2. Transient renewal failure: the primary cannot reach the Kubernetes API
//     server for renewDeadline (10s) and le.Run returns, but the lease TTL
//     (15s from last renewal) has not yet expired — no other pod has promoted.
//     We detect this by reading the lease after le.Run returns: if our pod is
//     still the HolderIdentity, the lease is intact and we loop back into
//     le.Run to resume renewal.
//
//  3. Preemption: the lease has expired and another pod has acquired it, or the
//     lease object no longer exists. In this case reading the lease reveals a
//     different (or empty) holder. We return a fatal error so controller-runtime
//     shuts down the manager and stops PostgreSQL.
//
// If the post-exit read itself fails (API server still unreachable), we log a
// warning and loop — we have no evidence of preemption, and the liveness probe
// isolation checker will fence us if we are genuinely isolated. A retryPeriod-
// sized timeout is used for the check to avoid blocking indefinitely.
//
// This design keeps the lease as a pure promotion synchronization mechanism.
// Network isolation fencing is left entirely to the liveness probe, which has
// access to replica connectivity information the lease mechanism lacks.
//
// Instance-level fencing (cnpg.io/fencedInstances) does not release the lease
// either: the operator deliberately skips switchover while the current primary
// is fenced, so holding the lease aligns with the freeze-the-cluster intent.
// Unfencing resumes the same primary without any lease transition.
func (r *Runnable) runLeaderElection(ctx context.Context) error {
	contextLogger := log.FromContext(ctx).WithName("primary-lease")

	for {
		le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
			Lock:            r.lock,
			LeaseDuration:   r.config.LeaseDuration,
			RenewDeadline:   r.config.RenewDeadline,
			RetryPeriod:     r.config.RetryPeriod,
			ReleaseOnCancel: false, // lease is released explicitly via Release()
			Name:            r.instance.GetClusterName(),
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(context.Context) {
					contextLogger.Info("Acquired primary lease")
					r.heldOnce.Do(func() { close(r.heldCh) })
				},
				OnStoppedLeading: func() {
					// leaderelection invokes this on every Run exit, including
					// clean ctx cancellation. The meaningful "we lost the lease"
					// signal is the fatal error returned from runLeaderElection.
					contextLogger.Debug("leaderelection.Run exited")
				},
				OnNewLeader: func(string) {},
			},
		})
		if err != nil {
			return err
		}

		le.Run(ctx)

		// Scenario 1: clean shutdown.
		if ctx.Err() != nil {
			return nil
		}

		// le.Run exited unexpectedly. Read the lease to distinguish a transient
		// renewal failure (we still hold it) from genuine preemption (another pod
		// holds it or the object is gone).
		checkCtx, checkCancel := context.WithTimeout(ctx, r.config.RetryPeriod)
		record, _, checkErr := r.lock.Get(checkCtx)
		checkCancel()

		switch classifyLeaseAfterRun(checkErr, record, r.lock.LockConfig.Identity) {
		case leaseMissing:
			// The lease object is gone (e.g. someone deleted it). The cluster
			// controller will recreate it on its next reconcile; loop and let
			// le.Run re-acquire it once it reappears.
			contextLogger.Warning("Primary lease object missing, waiting for it to be recreated")
		case leaseUnverifiable:
			// Cannot reach the API server to verify the holder. We have no evidence
			// of preemption, so loop back and let le.Run retry. If we are genuinely
			// isolated, the liveness probe isolation checker will fence us.
			contextLogger.Warning("Primary lease lost, cannot verify holder, retrying", "error", checkErr)
		case leasePreempted:
			// A different identity holds the lease — we have been preempted. This is
			// a terminal event: the returned error shuts down the manager and stops
			// PostgreSQL. Log it explicitly at the point of detection so the cause
			// is visible, rather than only surfacing as a generic manager error.
			err := fmt.Errorf("primary lease is now held by %q", record.HolderIdentity)
			contextLogger.Error(err, "Primary lease preempted, shutting down",
				"newHolder", record.HolderIdentity)
			return err
		case leaseStillHeld:
			// We still hold the lease — transient API server blip. Loop.
			contextLogger.Warning("Primary lease renewal failed transiently, retrying")
		}
	}
}
