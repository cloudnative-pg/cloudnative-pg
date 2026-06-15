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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
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
