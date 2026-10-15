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

package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/run/lease"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeLeaseAcquirer reports whatever acquirePrimaryLease's caller should see.
type fakeLeaseAcquirer struct{ err error }

func (f fakeLeaseAcquirer) Acquire(context.Context, lease.Config) error {
	return f.err
}

var _ = Describe("verifyPgDataCoherenceForPrimary primary lease gate", func() {
	const (
		namespace   = "default"
		clusterName = "cluster-example"
		podName     = clusterName + "-1"
	)

	newReconciler := func(acquireErr error) (*InstanceReconciler, *apiv1.Cluster) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Status:     apiv1.ClusterStatus{TargetPrimary: podName},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()

		pgInstance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(podName).
			WithClusterName(clusterName)
		pgInstance.PgData = GinkgoT().TempDir()

		return &InstanceReconciler{
			client:               fakeClient,
			instance:             pgInstance,
			primaryLeaseAcquirer: fakeLeaseAcquirer{err: acquireErr},
		}, cluster
	}

	It("blocks on the lease and does not mark itself primary while it's not held",
		func(ctx SpecContext) {
			r, cluster := newReconciler(context.DeadlineExceeded)

			err := r.verifyPgDataCoherenceForPrimary(ctx, cluster)

			Expect(err).To(MatchError(controller.ErrNextLoop))
			Expect(cluster.Status.CurrentPrimary).To(BeEmpty())
		})

	It("marks itself primary once the lease is held", func(ctx SpecContext) {
		r, cluster := newReconciler(nil)

		err := r.verifyPgDataCoherenceForPrimary(ctx, cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.CurrentPrimary).To(Equal(podName))
	})
})
