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
	"sync/atomic"
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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
		close(r.heldCh)

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

	It("keeps the lease held when an in-place instance-manager upgrade is in progress",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			createLease(ctx, kubeClient, thisPod)
			// An in-place online upgrade restarts the instance manager while
			// PostgreSQL keeps running, so this pod is still the primary and
			// must not blank the holder on its way out.
			r.instance.InstanceManagerIsUpgrading.Store(true)

			Expect(r.Release(ctx)).To(Succeed())
			// The holder is unchanged: the replacement instance manager re-adopts it.
			Expect(getHolder(ctx, kubeClient)).To(Equal(thisPod))
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
		close(r.heldCh)
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

var _ = Describe("Runnable watchdog", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
	)

	// Short timings give the specs many lock calls, under a silence limit of
	// 140ms that a busy test host does not stall the loop past.
	config := Config{
		LeaseDuration:         time.Hour,
		RenewDeadline:         100 * time.Millisecond,
		RetryPeriod:           20 * time.Millisecond,
		ReleasedLeaseDuration: time.Second,
	}

	// outage, when not zero, makes every lease call fail after that long, as
	// an API server that is down or does not answer would.
	var outage atomic.Int64

	// start runs the lease loop the way the instance manager does, through
	// Start and Acquire, and returns the function that stops it.
	start := func(ctx context.Context) (*Runnable, func()) {
		kubeClient := fake.NewClientset(&coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Spec:       coordinationv1.LeaseSpec{HolderIdentity: ptr.To(thisPod)},
		})
		kubeClient.PrependReactor("*", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
			delay := time.Duration(outage.Load())
			if delay == 0 {
				return false, nil, nil
			}
			time.Sleep(delay)
			return true, nil, errors.New("API server unavailable")
		})
		r := New(kubeClient, postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName))
		r.checkStepDown = func(*apiv1.Cluster, string) (bool, error) { return false, nil }

		runCtx, cancelRun := context.WithCancel(ctx)
		returned := make(chan struct{})
		go func() {
			defer close(returned)
			_ = r.Start(runCtx)
		}()
		Expect(r.Acquire(ctx, config)).To(Succeed())

		return r, func() {
			cancelRun()
			Eventually(returned).Should(BeClosed())
		}
	}

	BeforeEach(func() {
		outage.Store(0)
	})

	It("stays healthy while the loop renews the lease, and reports it stuck once the loop stops",
		func(ctx context.Context) {
			r, stop := start(ctx)
			Consistently(r.Watchdog().IsHealthy, 500*time.Millisecond).Should(Succeed())

			stop()
			Eventually(r.Watchdog().IsHealthy).Should(MatchError(ContainSubstring("primary lease loop")))
		})

	It("stays healthy while every lease call fails", func(ctx context.Context) {
		r, stop := start(ctx)
		defer stop()

		outage.Store(int64(time.Millisecond))
		Consistently(r.Watchdog().IsHealthy, 500*time.Millisecond).Should(Succeed())
	})

	It("stays healthy while lease calls take longer than the silence limit to fail", func(ctx context.Context) {
		r, stop := start(ctx)
		defer stop()

		outage.Store(int64(400 * time.Millisecond))
		Consistently(r.Watchdog().IsHealthy, time.Second).Should(Succeed())
	})

	It("stays healthy after a yield, so the liveness probe never restarts the replica", func(ctx context.Context) {
		r, stop := start(ctx)
		defer stop()

		r.Yield(ctx)
		Consistently(r.Watchdog().IsHealthy, 500*time.Millisecond).Should(Succeed())
	})
})

var _ = Describe("Runnable.observe", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
		otherPod    = "test-cluster-2"
	)

	// newRunnable returns a runnable of a cluster with a pending failover.
	newRunnable := func(kubeClient *fake.Clientset) *Runnable {
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: apiv1.PendingFailoverMarker}})
		return New(kubeClient, instance)
	}

	putLease := func(ctx context.Context, kubeClient *fake.Clientset, holder string) {
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To(holder),
				LeaseDurationSeconds: ptr.To(int32(15)),
				RenewTime:            ptr.To(metav1.NewMicroTime(time.Now())),
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	It("times a holder while the failover is pending, without writing", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		putLease(ctx, kubeClient, otherPod)
		before, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		r.observeLease(ctx, time.Second)
		Expect(r.observedRecord).NotTo(BeNil())
		Expect(r.observedRecord.HolderIdentity).To(Equal(otherPod))

		after, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
	})

	It("drops the observation when the failover is no longer pending", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		putLease(ctx, kubeClient, otherPod)
		r.observeLease(ctx, time.Second)
		Expect(r.observedRecord).NotTo(BeNil())

		r.instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: otherPod}})
		r.observeLease(ctx, time.Second)
		Expect(r.observedRecord).To(BeNil())
	})

	It("keeps the observation once this pod is the target", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		putLease(ctx, kubeClient, otherPod)
		r.observeLease(ctx, time.Second)
		Expect(r.observedRecord).NotTo(BeNil())

		r.instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: thisPod}})
		r.observeLease(ctx, time.Second)
		Expect(r.observedRecord).NotTo(BeNil())
	})

	It("makes Start read the lease while it waits for Acquire, without writing", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		r.instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: otherPod}})
		putLease(ctx, kubeClient, otherPod)
		leaseActions := func(verb string) int {
			count := 0
			for _, action := range kubeClient.Actions() {
				if action.GetResource().Resource == "leases" && action.GetVerb() == verb {
					count++
				}
			}
			return count
		}

		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = r.Start(runCtx)
		}()
		defer func() {
			cancel()
			<-done
		}()

		Consistently(func() int { return leaseActions("get") }, 300*time.Millisecond).Should(BeZero())

		r.instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: apiv1.PendingFailoverMarker}})
		// The next poll comes within the default retry period.
		Eventually(func() int { return leaseActions("get") }, 3*time.Second).Should(BeNumerically(">", 0))
		Expect(leaseActions("update")).To(BeZero())
		Expect(leaseActions("create")).To(Equal(1))
	})

	It("has nothing to time on a released record or one naming this pod", func(ctx context.Context) {
		for _, holder := range []string{"", thisPod} {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, holder)

			r.observeLease(ctx, time.Second)
			Expect(r.observedRecord).To(BeNil())
		}
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
		// This pod is the target primary unless a spec says otherwise: only the
		// target may take a lease over.
		instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: thisPod}})
		r := New(kubeClient, instance)
		// Set a realistic lease duration so the expiry math is exercised.
		r.config.LeaseDuration = 15 * time.Second
		return r
	}

	// setTarget changes the target primary the instance manager has reconciled.
	setTarget := func(r *Runnable, target string) {
		r.instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: target}})
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

	It("refreshes the record with a write when this pod is already the holder", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		putLease(ctx, kubeClient, thisPod, time.Now().Add(-10*time.Second), 15)
		before := getLease(ctx, kubeClient)

		acquired, err := r.tryTakeOver(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(acquired).To(BeTrue())

		after := getLease(ctx, kubeClient)
		Expect(after.Spec.RenewTime.After(before.Spec.RenewTime.Time)).To(BeTrue())
		// Re-adopting our own record is not a transition.
		Expect(ptr.Deref(after.Spec.HolderIdentity, "")).To(Equal(thisPod))
		Expect(ptr.Deref(after.Spec.LeaseTransitions, 0)).To(BeEquivalentTo(3))
		// The write is what the monitor ages the lease from.
		Expect(r.lastConfirmed.Load()).NotTo(BeNil())
	})

	It("does not report the lease as held when a challenger claims it first", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		putLease(ctx, kubeClient, thisPod, time.Now().Add(-10*time.Second), 15)

		// The challenger's claim lands between our read and our write, so our
		// conditional write fails with a conflict.
		kubeClient.PrependReactor("update", "leases",
			func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewConflict(
					schema.GroupResource{Resource: "leases"}, clusterName,
					errors.New("the object has been modified"))
			})

		acquired, err := r.tryTakeOver(ctx)
		Expect(apierrors.IsConflict(err)).To(BeTrue())
		Expect(acquired).To(BeFalse())
		Expect(r.lastConfirmed.Load()).To(BeNil())
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

	It("does not take over a released lease while another pod is the target primary", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		setTarget(r, otherPod)
		putLease(ctx, kubeClient, "", time.Now(), 15)

		acquired, err := r.tryTakeOver(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(acquired).To(BeFalse())
		Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(BeEmpty())
	})

	It("does not start the observation window while another pod is the target primary",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, otherPod)

			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(r.observedRecord).To(BeNil())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(otherPod))
		})

	It("times the record while a failover is pending, but never claims it",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, apiv1.PendingFailoverMarker)

			observe(ctx, r)
			Expect(r.observedRecord).NotTo(BeNil())

			// The window is ripe, but the marker names nobody as target.
			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(otherPod))
		})

	It("takes over as soon as it is elected, when the window started at the pending marker",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, apiv1.PendingFailoverMarker)
			observe(ctx, r)

			setTarget(r, thisPod)
			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeTrue())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(thisPod))
		})

	It("drops a window started at the pending marker when another pod becomes the target",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, apiv1.PendingFailoverMarker)
			observe(ctx, r)

			setTarget(r, otherPod)
			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(r.observedRecord).To(BeNil())

			// Becoming the target afterwards starts a new window.
			setTarget(r, thisPod)
			acquired, err = r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(r.observedRecord).NotTo(BeNil())
		})

	It("never claims while yielded, and drops the window it had timed",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, apiv1.PendingFailoverMarker)
			observe(ctx, r)

			setTarget(r, thisPod)
			r.yielded.Store(true)
			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(r.observedRecord).To(BeNil())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(otherPod))
		})

	It("restarts the window when the holder renews during the pending failover",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, apiv1.PendingFailoverMarker)
			observe(ctx, r)
			renewLease(ctx, kubeClient, otherPod, time.Now())

			setTarget(r, thisPod)
			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(otherPod))
		})

	It("waits a full window after becoming the target before it takes over a stale holder",
		func(ctx context.Context) {
			kubeClient := fake.NewClientset()
			r := newRunnable(kubeClient)
			putLease(ctx, kubeClient, otherPod, time.Now().Add(-20*time.Second), 15)
			setTarget(r, otherPod)
			for range 3 {
				_, err := r.tryTakeOver(ctx)
				Expect(err).NotTo(HaveOccurred())
			}
			// Whatever the polls recorded is older than the window by now.
			r.observedTime = time.Now().Add(-r.config.LeaseDuration - time.Second)

			// A reconcile stores this pod as the target: the holder was stale long
			// before, but the old primary may only now learn that it was replaced.
			setTarget(r, thisPod)
			acquired, err := r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeFalse())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(otherPod))

			r.observedTime = time.Now().Add(-r.config.LeaseDuration - time.Second)
			acquired, err = r.tryTakeOver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeTrue())
			Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(thisPod))
		})

	It("converges through the polling loop once a reconcile makes this pod the target", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		r.config.LeaseDuration = 50 * time.Millisecond
		r.config.RenewDeadline = 40 * time.Millisecond
		r.config.RetryPeriod = 5 * time.Millisecond
		setTarget(r, otherPod)
		putLease(ctx, kubeClient, otherPod, time.Now(), 15)

		acquired := make(chan error, 1)
		go func() { acquired <- r.preAcquire(ctx) }()

		// Well past the window, and still no claim.
		Consistently(acquired, 200*time.Millisecond).ShouldNot(Receive())

		setTarget(r, thisPod)

		Eventually(acquired).Should(Receive(BeNil()))
		Expect(ptr.Deref(getLease(ctx, kubeClient).Spec.HolderIdentity, "")).To(Equal(thisPod))
	})

	It("keeps renewing its own lease after another pod became the target primary", func(ctx context.Context) {
		kubeClient := fake.NewClientset()
		r := newRunnable(kubeClient)
		setTarget(r, otherPod)
		putLease(ctx, kubeClient, thisPod, time.Now().Add(-10*time.Second), 15)

		acquired, err := r.tryTakeOver(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(acquired).To(BeTrue())
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

	It("bounds the Get call so it cannot block past RenewDeadline when the API server is unreachable",
		func(ctx context.Context) {
			r := newRunnable(fake.NewClientset())
			r.config.RenewDeadline = 50 * time.Millisecond
			r.lock = &blockingLock{}

			start := time.Now()
			acquired, err := r.tryTakeOver(ctx)

			// The lock never actually replies: the only way this call returns
			// is the per-call timeout added around it, well before
			// blockingLock's own 2s safety net would fire.
			Expect(time.Since(start)).To(BeNumerically("<", time.Second))
			Expect(acquired).To(BeFalse())
			Expect(err).To(MatchError(context.DeadlineExceeded))
		})

	It("takes the lease over when the API server answers well within RenewDeadline",
		func(ctx context.Context) {
			r := newRunnable(fake.NewClientset())
			// tryTakeOver ignores RetryPeriod. We set it small anyway, so
			// this test still works if the default value changes later.
			r.config.RetryPeriod = 20 * time.Millisecond
			r.config.RenewDeadline = 600 * time.Millisecond
			r.lock = &slowLock{getDelay: 400 * time.Millisecond}

			acquired, err := r.tryTakeOver(ctx)

			// getDelay is bigger than RetryPeriod, smaller than
			// RenewDeadline. So this test only passes if the code waits
			// for RenewDeadline, not the shorter, wrong bound from an
			// earlier version of this fix.
			Expect(err).NotTo(HaveOccurred())
			Expect(acquired).To(BeTrue())
		})
})

// slowLock is a resourcelock.Interface that answers, but only after getDelay,
// simulating an API server under load rather than an unreachable one. The
// record it returns has an empty holder, the cleanly-released state that
// tryTakeOver claims straight away.
type slowLock struct {
	getDelay time.Duration
}

func (l *slowLock) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-time.After(l.getDelay):
		return &resourcelock.LeaderElectionRecord{HolderIdentity: ""}, nil, nil
	}
}

func (*slowLock) Create(context.Context, resourcelock.LeaderElectionRecord) error { return nil }
func (*slowLock) Update(context.Context, resourcelock.LeaderElectionRecord) error { return nil }
func (*slowLock) RecordEvent(string)                                              {}

// Identity is never empty: an empty identity would make the record read above
// look like one we already hold, short-circuiting the claim under test.
func (*slowLock) Identity() string { return "slow-lock" }
func (*slowLock) Describe() string { return "slowLock" }

// blockingLock is a resourcelock.Interface whose Get never replies on its
// own, simulating an API server that accepts the connection but never
// answers. It exists to prove tryTakeOver bounds the call itself rather
// than depending on the caller's context already having a deadline.
type blockingLock struct{}

func (*blockingLock) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-time.After(2 * time.Second):
		// Safety net: fails the test with a clear message instead of
		// hanging forever if the per-call bound regresses.
		return nil, nil, errors.New("blockingLock.Get: not bounded by a per-call timeout")
	}
}

func (*blockingLock) Create(context.Context, resourcelock.LeaderElectionRecord) error { return nil }
func (*blockingLock) Update(context.Context, resourcelock.LeaderElectionRecord) error { return nil }
func (*blockingLock) RecordEvent(string)                                              {}
func (*blockingLock) Identity() string                                                { return "" }
func (*blockingLock) Describe() string                                                { return "blockingLock" }

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

var _ = Describe("shouldStepDown", func() {
	const ourIdentity = "test-cluster-1"

	newCluster := func(enabled bool, instances int) *apiv1.Cluster {
		return &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: instances,
				Probes: &apiv1.ProbesConfiguration{
					Liveness: &apiv1.LivenessProbe{
						IsolationCheck: &apiv1.IsolationCheckConfiguration{
							Enabled:           ptr.To(enabled),
							RequestTimeout:    1000,
							ConnectionTimeout: 1000,
						},
					},
				},
			},
		}
	}

	It("skips the check when isolation checking is disabled", func() {
		stepDown, err := shouldStepDown(newCluster(false, 3), ourIdentity)
		Expect(err).NotTo(HaveOccurred())
		Expect(stepDown).To(BeFalse())
	})

	It("skips the check when the cluster has a single instance", func() {
		stepDown, err := shouldStepDown(newCluster(true, 1), ourIdentity)
		Expect(err).NotTo(HaveOccurred())
		Expect(stepDown).To(BeFalse())
	})

	It("skips the check when no probes configuration is set", func() {
		stepDown, err := shouldStepDown(&apiv1.Cluster{Spec: apiv1.ClusterSpec{Instances: 3}}, ourIdentity)
		Expect(err).NotTo(HaveOccurred())
		Expect(stepDown).To(BeFalse())
	})

	It("fails open when the reachability checker cannot be built", func() {
		// In the test environment there is no server CA certificate on disk,
		// so building the checker fails; this must not be reported as a step-down.
		stepDown, err := shouldStepDown(newCluster(true, 3), ourIdentity)
		Expect(err).To(HaveOccurred())
		Expect(stepDown).To(BeFalse())
	})
})

// held reports whether the lease is held: taken by the run loop and not yielded
// since.
func (r *Runnable) held() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.heldCh:
		return true
	default:
		return false
	}
}

var _ = Describe("Runnable.Yield", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
	)

	var (
		kubeClient *fake.Clientset
		r          *Runnable
		hardStops  atomic.Int32
		stopRun    func()
	)

	getLease := func(ctx context.Context) *coordinationv1.Lease {
		lease, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		return lease
	}

	// A released lease, with this pod as the target: the run loop claims it on
	// its first poll.
	BeforeEach(func() {
		kubeClient = fake.NewClientset(&coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Spec:       coordinationv1.LeaseSpec{HolderIdentity: ptr.To("")},
		})
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: thisPod}})
		r = New(kubeClient, instance)
		r.config.RetryPeriod = 10 * time.Millisecond
		r.config.RenewDeadline = 50 * time.Millisecond
		r.config.LeaseDuration = 200 * time.Millisecond
		// Any stale lease is a step-down; only a running monitor can get here.
		r.checkStepDown = func(*apiv1.Cluster, string) (bool, error) { return true, nil }
		hardStops.Store(0)
		r.hardStop = func() { hardStops.Add(1) }

		// Not the node's ctx: Ginkgo cancels that one when BeforeEach returns,
		// and the loop has to outlive it.
		runCtx, cancelRun := context.WithCancel(context.Background())
		returned := make(chan struct{})
		go func(r *Runnable) {
			defer GinkgoRecover()
			defer close(returned)
			Expect(r.Start(runCtx)).To(Succeed())
		}(r)
		stopRun = func() {
			cancelRun()
			Eventually(returned).Should(BeClosed())
		}
		DeferCleanup(func() { stopRun() })
	})

	It("stops renewing without writing, and renews again only after the next Acquire", func(ctx context.Context) {
		Expect(r.Acquire(ctx, r.config)).To(Succeed())
		r.Yield(ctx)
		Expect(r.held()).To(BeFalse())

		// At most the write in flight at the Yield still lands: over the
		// window the RenewTime changes once at most, then the record keeps
		// naming this pod unchanged, so the new target sees it go stale.
		seen := map[time.Time]struct{}{}
		Consistently(func() int {
			seen[getLease(ctx).Spec.RenewTime.Time] = struct{}{}
			return len(seen)
		}, r.config.LeaseDuration+r.config.RenewDeadline).Should(BeNumerically("<=", 2))
		Expect(ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")).To(Equal(thisPod))
		Expect(r.held()).To(BeFalse())

		renewedAt := getLease(ctx).Spec.RenewTime
		acquireCtx, cancel := context.WithTimeout(ctx, 3*r.config.LeaseDuration)
		defer cancel()
		Expect(r.Acquire(acquireCtx, r.config)).To(Succeed())
		Expect(ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")).To(Equal(thisPod))
		Expect(getLease(ctx).Spec.RenewTime.After(renewedAt.Time)).To(BeTrue())
	})

	It("leaves a hold that is being taken unpublished when the yield comes in during the write",
		func(ctx context.Context) {
			kubeClient.PrependReactor("update", "leases",
				func(k8stesting.Action) (bool, runtime.Object, error) {
					r.Yield(ctx)
					return false, nil, nil
				})
			acquireCtx, cancel := context.WithTimeout(ctx, 3*r.config.LeaseDuration)
			defer cancel()
			Expect(r.Acquire(acquireCtx, r.config)).To(MatchError(context.DeadlineExceeded))
			Expect(ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")).To(Equal(thisPod))
			Consistently(hardStops.Load, r.config.LeaseDuration).Should(BeZero())
		})

	It("does not blank the record at process exit, so the new target waits the full window",
		func(ctx context.Context) {
			Expect(r.Acquire(ctx, r.config)).To(Succeed())
			r.Yield(ctx)
			// As in cmd.go, Release runs once the runnables have stopped.
			stopRun()
			Expect(r.Release(ctx)).To(Succeed())
			Expect(ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")).To(Equal(thisPod))
		})

	It("lets the new target take the lease over, and never writes it again", func(ctx context.Context) {
		const newTarget = "test-cluster-3"
		Expect(r.Acquire(ctx, r.config)).To(Succeed())

		// A second instance manager, on the same lease.
		other := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(newTarget).
			WithClusterName(clusterName)
		r3 := New(kubeClient, other)
		r3.config = r.config
		r3.checkStepDown = r.checkStepDown
		r3.hardStop = r.hardStop
		runCtx, cancelRun := context.WithCancel(context.Background())
		returned := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(returned)
			Expect(r3.Start(runCtx)).To(Succeed())
		}()
		DeferCleanup(func() {
			cancelRun()
			Eventually(returned).Should(BeClosed())
		})

		// The operator names the other pod as target; both reconcilers see it.
		target := &apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: newTarget}}
		r.instance.SetCluster(target)
		other.SetCluster(target)
		r.Yield(ctx)

		acquireCtx, cancel := context.WithTimeout(ctx, 3*r.config.LeaseDuration)
		defer cancel()
		Expect(r3.Acquire(acquireCtx, r3.config)).To(Succeed())
		Expect(ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")).To(Equal(newTarget))

		// The yielded pod polls on but never writes: the holder stays the
		// new target, which renews it.
		Consistently(func() string {
			return ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")
		}, 2*r.config.LeaseDuration).Should(Equal(newTarget))
		Expect(r.held()).To(BeFalse())
		Expect(hardStops.Load()).To(BeZero())
	})

	It("does not claim the lease when the Yield comes during the take-over read", func(ctx context.Context) {
		// The read can take up to RenewDeadline; the write that follows it
		// must still honour a Yield that came meanwhile.
		reading := make(chan struct{})
		release := make(chan struct{})
		var blocked atomic.Bool
		kubeClient.PrependReactor("get", "leases",
			func(k8stesting.Action) (bool, runtime.Object, error) {
				if blocked.CompareAndSwap(false, true) {
					close(reading)
					<-release
				}
				return false, nil, nil
			})
		acquireCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			defer GinkgoRecover()
			_ = r.Acquire(acquireCtx, r.config)
		}()

		Eventually(reading).Should(BeClosed())
		r.Yield(ctx)
		close(release)

		Consistently(func() string {
			return ptr.Deref(getLease(ctx).Spec.HolderIdentity, "")
		}, r.config.LeaseDuration).Should(BeEmpty())
	})

	It("stops the monitor of a hold that survived a transient renewal failure", func(ctx context.Context) {
		// A renewal loop that gives up and is re-entered is the same hold, so
		// the monitor Yield stops must still be the one watching it.
		r.config.RenewDeadline = 300 * time.Millisecond
		r.config.LeaseDuration = 2 * time.Second
		var rejectWrites atomic.Bool
		kubeClient.PrependReactor("update", "leases",
			func(k8stesting.Action) (bool, runtime.Object, error) {
				if rejectWrites.Load() {
					return true, nil, apierrors.NewInternalError(errors.New("api server down"))
				}
				return false, nil, nil
			})
		Expect(r.Acquire(ctx, r.config)).To(Succeed())
		// The renewal loop only gives up once it is renewing: its first write
		// is an acquire, which it retries for as long as it takes.
		renewedAt := getLease(ctx).Spec.RenewTime
		Eventually(func() bool {
			return getLease(ctx).Spec.RenewTime.After(renewedAt.Time)
		}).Should(BeTrue())

		// Long enough for the renewal loop to give up, then let it renew again:
		// once from the take-over poll, then from the re-entered renewal loop.
		rejectWrites.Store(true)
		time.Sleep(2 * r.config.RenewDeadline)
		rejectWrites.Store(false)
		seen := map[time.Time]struct{}{}
		Eventually(func() int {
			seen[getLease(ctx).Spec.RenewTime.Time] = struct{}{}
			return len(seen)
		}).Should(BeNumerically(">=", 3))

		r.Yield(ctx)
		Consistently(hardStops.Load, r.config.LeaseDuration+r.config.RenewDeadline).Should(BeZero())
	})
})
