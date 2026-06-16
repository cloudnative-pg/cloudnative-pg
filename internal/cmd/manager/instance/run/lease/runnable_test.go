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
	"errors"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Runnable.Release", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
		otherPod    = "test-cluster-2"
	)

	newRunnable := func(kubeClient *fake.Clientset) *Runnable {
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		return New(kubeClient, instance)
	}

	createLease := func(ctx context.Context, kubeClient *fake.Clientset, holder string) {
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterName,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To(holder),
				LeaseDurationSeconds: ptr.To(int32(15)),
			},
		}
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	getHolder := func(ctx context.Context, kubeClient *fake.Clientset) string {
		lease, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		if lease.Spec.HolderIdentity == nil {
			return ""
		}
		return *lease.Spec.HolderIdentity
	}

	getDuration := func(ctx context.Context, kubeClient *fake.Clientset) *int32 {
		lease, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		return lease.Spec.LeaseDurationSeconds
	}

	It("is a no-op when the lease object does not exist", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)

		Expect(r.Release(ctx)).To(Succeed())
	})

	It("is a no-op when another pod holds the lease", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		createLease(ctx, kubeClient, otherPod)

		Expect(r.Release(ctx)).To(Succeed())
		Expect(getHolder(ctx, kubeClient)).To(Equal(otherPod))
	})

	It("releases the lease when this pod is the current holder (acquired by this run)", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		createLease(ctx, kubeClient, thisPod)
		// Simulate the lease having been acquired by this run.
		r.heldOnce.Do(func() { close(r.heldCh) })

		Expect(r.Release(ctx)).To(Succeed())
		Expect(getHolder(ctx, kubeClient)).To(BeEmpty())
	})

	It("releases the lease when this pod is the current holder even if heldCh was never closed",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			createLease(ctx, kubeClient, thisPod)
			// heldCh is intentionally left open — simulates a pod restart where we are
			// already the lease holder but Acquire was never called in this process run.

			Expect(r.Release(ctx)).To(Succeed())
			Expect(getHolder(ctx, kubeClient)).To(BeEmpty())
		})

	It("writes the default released-lease TTL when no config was applied", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		createLease(ctx, kubeClient, thisPod)

		Expect(r.Release(ctx)).To(Succeed())
		Expect(getDuration(ctx, kubeClient)).To(Equal(ptr.To(int32(1))))
	})

	It("writes the configured released-lease TTL", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		createLease(ctx, kubeClient, thisPod)
		// Simulate the config captured by a previous Acquire call.
		r.config.ReleasedLeaseDuration = 5 * time.Second

		Expect(r.Release(ctx)).To(Succeed())
		Expect(getHolder(ctx, kubeClient)).To(BeEmpty())
		Expect(getDuration(ctx, kubeClient)).To(Equal(ptr.To(int32(5))))
	})

	It("preserves leaseTransitions across release", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		// Seed a lease with a non-zero transition counter, as a steady-state
		// cluster that has experienced several primary handovers would.
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterName,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To(thisPod),
				LeaseDurationSeconds: ptr.To(int32(15)),
				LeaseTransitions:     ptr.To(int32(7)),
			},
		}
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(r.Release(ctx)).To(Succeed())

		released, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(released.Spec.LeaseTransitions).To(Equal(ptr.To(int32(7))))
	})
})

var _ = Describe("Runnable.Acquire", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
	)

	var first, second Config

	newRunnable := func() *Runnable {
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		return New(fake.NewClientset(), instance)
	}

	// markHeld simulates the lease having been acquired so Acquire can return
	// without a running leader-election loop behind it.
	markHeld := func(r *Runnable) {
		r.heldOnce.Do(func() { close(r.heldCh) })
	}

	BeforeEach(func() {
		first = Config{
			LeaseDuration:         30 * time.Second,
			RenewDeadline:         20 * time.Second,
			RetryPeriod:           4 * time.Second,
			ReleasedLeaseDuration: 2 * time.Second,
		}
		second = Config{
			LeaseDuration:         60 * time.Second,
			RenewDeadline:         40 * time.Second,
			RetryPeriod:           8 * time.Second,
			ReleasedLeaseDuration: 5 * time.Second,
		}
	})

	It("returns nil once the lease is held", func(ctx context.Context) {
		r := newRunnable()
		markHeld(r)

		Expect(r.Acquire(ctx, first)).To(Succeed())
	})

	It("captures the configuration and activates the runnable on the first call", func(ctx context.Context) {
		r := newRunnable()
		markHeld(r)

		Expect(r.Acquire(ctx, first)).To(Succeed())
		Expect(r.config).To(Equal(first))
		// activateCh is closed so a started Start() would proceed to the election loop.
		Expect(r.activateCh).To(BeClosed())
	})

	It("ignores the configuration supplied by later calls", func(ctx context.Context) {
		r := newRunnable()
		markHeld(r)

		Expect(r.Acquire(ctx, first)).To(Succeed())
		Expect(r.Acquire(ctx, second)).To(Succeed())
		// The second config must be dropped: timings are captured once, at activation.
		Expect(r.config).To(Equal(first))
	})

	It("returns the context error when the lease is not acquired before the deadline",
		func(specCtx context.Context) {
			r := newRunnable()
			// heldCh is never closed: no leader-election loop is running here, so the
			// only way Acquire returns is by hitting the caller-provided deadline.
			acquireCtx, cancel := context.WithTimeout(specCtx, 50*time.Millisecond)
			defer cancel()

			Expect(r.Acquire(acquireCtx, first)).To(MatchError(context.DeadlineExceeded))
		})
})

var _ = Describe("Runnable.tryTakeOver", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
		otherPod    = "test-cluster-2"
	)

	newRunnable := func(kubeClient *fake.Clientset) *Runnable {
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		r := New(kubeClient, instance)
		// Set a realistic lease duration so the expiry math is exercised.
		r.config.LeaseDuration = 15 * time.Second
		return r
	}

	// putLease writes a lease object with the given holder, renew time and TTL.
	putLease := func(
		ctx context.Context,
		kubeClient *fake.Clientset,
		holder string,
		renewedAt time.Time,
		ttlSeconds int32,
	) {
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterName,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To(holder),
				LeaseDurationSeconds: ptr.To(ttlSeconds),
				RenewTime:            ptr.To(metav1.NewMicroTime(renewedAt)),
				LeaseTransitions:     ptr.To(int32(3)),
			},
		}
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	getLease := func(ctx context.Context, kubeClient *fake.Clientset) *coordinationv1.Lease {
		lease, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		return lease
	}

	// renewLease rewrites the holder and renew time of an existing lease, as a
	// live holder renewing its lease would.
	renewLease := func(ctx context.Context, kubeClient *fake.Clientset, holder string, renewedAt time.Time) {
		lease := getLease(ctx, kubeClient)
		lease.Spec.HolderIdentity = ptr.To(holder)
		lease.Spec.RenewTime = ptr.To(metav1.NewMicroTime(renewedAt))
		_, err := kubeClient.CoordinationV1().Leases(namespace).Update(ctx, lease, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	// observe drives a first tryTakeOver so the runnable records the current
	// lease, then backdates the observation so the next call sees the
	// LeaseDuration window as already elapsed.
	observe := func(ctx context.Context, r *Runnable) {
		acquired, err := r.tryTakeOver(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(acquired).To(BeFalse())
		r.observedTime = time.Now().Add(-r.config.LeaseDuration - time.Second)
	}

	It("returns false and clears the observation when the lease object does not exist",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			// Pre-seed an observation to prove a missing lease resets it.
			r.observedRecord = &resourcelock.LeaderElectionRecord{HolderIdentity: otherPod}

			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(r.observedRecord).To(BeNil())
		})

	It("returns true and does not write when this pod is already the holder", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		putLease(ctx, kubeClient, thisPod, time.Now(), 15)
		before := getLease(ctx, kubeClient).ResourceVersion

		acquired, err := r.tryTakeOver(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(acquired).To(BeTrue())
		// No write happened: ResourceVersion is unchanged.
		Expect(getLease(ctx, kubeClient).ResourceVersion).To(Equal(before))
	})

	It("refuses on the first sighting of another holder and records the observation",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)

			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			// The observation is recorded but no write happened.
			Expect(r.observedRecord).NotTo(BeNil())
			Expect(*getLease(ctx, kubeClient).Spec.HolderIdentity).To(Equal(otherPod))
		})

	It("takes over another holder only after observing it unchanged for LeaseDuration",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)

			observe(ctx, r)

			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeTrue())

			lease := getLease(ctx, kubeClient)
			Expect(*lease.Spec.HolderIdentity).To(Equal(thisPod))
			// LeaseTransitions must be bumped to record the hand-over.
			Expect(*lease.Spec.LeaseTransitions).To(Equal(int32(4)))
			// LeaseDurationSeconds is rewritten to our configured value.
			Expect(*lease.Spec.LeaseDurationSeconds).To(Equal(int32(15)))
		})

	It("never takes over a holder that keeps renewing, however long we wait",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)

			observe(ctx, r)
			// The holder renews just before our next look: the record changes,
			// so the observation must reset and we must not preempt it.
			renewLease(ctx, kubeClient, otherPod, time.Now())

			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(*getLease(ctx, kubeClient).Spec.HolderIdentity).To(Equal(otherPod))
		})

	It("takes over immediately when the lease has an empty holder (cleanly released)",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			// An empty holder marks the lease as free regardless of RenewTime,
			// with no observation window.
			putLease(ctx, kubeClient, "", time.Now(), 1)

			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeTrue())

			lease := getLease(ctx, kubeClient)
			Expect(*lease.Spec.HolderIdentity).To(Equal(thisPod))
			Expect(*lease.Spec.LeaseTransitions).To(Equal(int32(4)))
		})

	It("reports not-acquired when a competing candidate wins the take-over race",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)

			observe(ctx, r)

			// Simulate the other candidate winning the optimistic-concurrency
			// race: our conditional Update fails with a conflict.
			kubeClient.PrependReactor("update", "leases",
				func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewConflict(
						schema.GroupResource{Resource: "leases"}, clusterName,
						errors.New("the object has been modified"))
				})

			acquired, err := r.tryTakeOver(ctx)
			Expect(apierrors.IsConflict(err)).To(BeTrue())
			Expect(acquired).To(BeFalse())
		})
})

var _ = Describe("classifyLeaseAfterRun", func() {
	const ourIdentity = "test-cluster-1"

	record := func(holder string) *resourcelock.LeaderElectionRecord {
		return &resourcelock.LeaderElectionRecord{HolderIdentity: holder}
	}

	It("treats a missing lease as recoverable (retry)", func() {
		notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "leases"}, "test-cluster")
		Expect(classifyLeaseAfterRun(notFound, nil, ourIdentity)).To(Equal(leaseMissing))
	})

	It("treats any other read error as unverifiable (retry, no fencing)", func() {
		Expect(classifyLeaseAfterRun(errors.New("connection refused"), nil, ourIdentity)).
			To(Equal(leaseUnverifiable))
	})

	It("treats a different holder as preemption (the branch that stops PostgreSQL)", func() {
		Expect(classifyLeaseAfterRun(nil, record("test-cluster-2"), ourIdentity)).
			To(Equal(leasePreempted))
	})

	It("treats a cleared holder as preemption", func() {
		// An empty holder means we no longer own the lease; we must not keep
		// running as primary.
		Expect(classifyLeaseAfterRun(nil, record(""), ourIdentity)).To(Equal(leasePreempted))
	})

	It("treats our own identity as still held (transient blip, retry)", func() {
		Expect(classifyLeaseAfterRun(nil, record(ourIdentity), ourIdentity)).
			To(Equal(leaseStillHeld))
	})
})
