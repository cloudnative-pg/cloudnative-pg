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

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
})
