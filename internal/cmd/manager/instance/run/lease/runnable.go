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
	"sync/atomic"
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
	// leaseDuration is how long a lease is valid before it expires.
	leaseDuration = 15 * time.Second

	// renewDeadline is how long the holder tries to renew before giving up.
	renewDeadline = 10 * time.Second

	// retryPeriod is how frequently a non-holder retries acquiring the lease.
	retryPeriod = 2 * time.Second

	// releasedLeaseDurationSeconds is the TTL written when explicitly releasing
	// the lease. An empty HolderIdentity already marks the lease as free, but
	// setting the duration to 1 second mirrors the k8s leaderelection.release()
	// behaviour and acts as belt-and-suspenders: even if an acquirer does not
	// treat an empty identity as immediately available, it waits at most one
	// second before the TTL expires and it can take the lease.
	releasedLeaseDurationSeconds = 1
)

// Runnable manages the primary lease for this instance.
// It starts idle and enters the acquisition/renewal loop only after Acquire is called.
type Runnable struct {
	instance *postgres.Instance
	lock     *resourcelock.LeaseLock

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
		lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Namespace: instance.GetNamespaceName(),
				Name:      instance.GetClusterName(),
			},
			Client: kubeClient.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: instance.GetPodName(),
			},
		},
		activateCh: make(chan struct{}),
		heldCh:     make(chan struct{}),
	}
}

// Acquire signals the runnable to start competing for the lease,
// then blocks until the lease is held or ctx is cancelled.
func (r *Runnable) Acquire(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	r.activateOnce.Do(func() {
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
// TODO(Step 4): block here until Postgres is truly down before releasing.
// postgres.Instance has no public "wait for postmaster to exit" API yet — see
// the note in Step 4 of the implementation plan.
func (r *Runnable) Release(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Releasing primary lease")

	record, _, err := r.lock.Get(ctx)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.HolderIdentity != r.lock.LockConfig.Identity {
		return nil
	}
	return r.lock.Update(ctx, resourcelock.LeaderElectionRecord{
		LeaseDurationSeconds: releasedLeaseDurationSeconds,
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
func (r *Runnable) runLeaderElection(ctx context.Context) error {
	contextLogger := log.FromContext(ctx).WithName("primary-lease")

	// becameLeader is set to true the moment we first acquire the lease.
	// It is stored from the OnStartedLeading goroutine and read after le.Run
	// returns — well after the first renewal cycle — so the write always
	// happens before the read.
	var becameLeader atomic.Bool

	for {
		le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
			Lock:            r.lock,
			LeaseDuration:   leaseDuration,
			RenewDeadline:   renewDeadline,
			RetryPeriod:     retryPeriod,
			ReleaseOnCancel: false, // lease is released explicitly via Release()
			Name:            r.instance.GetClusterName(),
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(context.Context) {
					contextLogger.Info("Acquired primary lease")
					becameLeader.Store(true)
					r.heldOnce.Do(func() { close(r.heldCh) })
				},
				OnStoppedLeading: func() {
					contextLogger.Warning("Primary lease lost")
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
		checkCtx, checkCancel := context.WithTimeout(ctx, retryPeriod)
		record, _, checkErr := r.lock.Get(checkCtx)
		checkCancel()

		if checkErr != nil {
			// Cannot reach the API server to verify the holder. We have no evidence
			// of preemption, so loop back and let le.Run retry. If we are genuinely
			// isolated, the liveness probe isolation checker will fence us.
			contextLogger.Warning("Primary lease lost, cannot verify holder, retrying", "error", checkErr)
			continue
		}
		if record.HolderIdentity != r.lock.LockConfig.Identity {
			// Another pod holds the lease — we have been preempted.
			return fmt.Errorf("primary lease is now held by %q", record.HolderIdentity)
		}

		// We still hold the lease — transient API server blip. Loop.
		contextLogger.Warning("Primary lease renewal failed transiently, retrying")
	}
}
