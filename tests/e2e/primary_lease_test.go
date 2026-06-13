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

package e2e

import (
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The primary lease is the per-cluster Kubernetes Lease that the operator
// creates and the primary instance must hold to act as PostgreSQL primary.
var _ = Describe("Primary lease", Label(tests.LabelSelfHealing), func() {
	const (
		sampleFile  = fixturesDir + "/primary_lease/cluster-primary-lease.yaml.template"
		clusterName = "cluster-primary-lease"
		level       = tests.Medium

		// Must match .spec.primaryLease.leaseDurationSeconds in the fixture.
		expectedLeaseDurationSeconds int32 = 30
	)
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// getLease fetches the cluster's Lease object.
	getLease := func(g Gomega) *coordinationv1.Lease {
		lease := &coordinationv1.Lease{}
		err := env.Client.Get(env.Ctx,
			ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName}, lease)
		g.Expect(err).ToNot(HaveOccurred())
		return lease
	}

	It("creates, owns and tracks the primary lease", func() {
		var err error
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "primary-lease")
		Expect(err).ToNot(HaveOccurred())

		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		var currentPrimary string
		By("electing a primary", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			currentPrimary = cluster.Status.CurrentPrimary
			Expect(currentPrimary).ToNot(BeEmpty())
		})

		By("creating a Lease named after the cluster, held by the current primary", func() {
			Eventually(func(g Gomega) {
				lease := getLease(g)
				g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
				g.Expect(*lease.Spec.HolderIdentity).To(Equal(currentPrimary))
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
		})

		By("propagating the configured lease duration to the Lease object", func() {
			// client-go's leader election writes int(LeaseDuration/second) into
			// the Lease on every acquire/renew, so this proves .spec.primaryLease
			// reaches the running instance manager.
			Eventually(func(g Gomega) {
				lease := getLease(g)
				g.Expect(lease.Spec.LeaseDurationSeconds).ToNot(BeNil())
				g.Expect(*lease.Spec.LeaseDurationSeconds).To(Equal(expectedLeaseDurationSeconds))
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
		})

		By("owning the Lease via the Cluster", func() {
			lease := getLease(Default)
			Expect(lease.OwnerReferences).ToNot(BeEmpty())
			Expect(lease.OwnerReferences[0].Kind).To(Equal(apiv1.ClusterKind))
			Expect(lease.OwnerReferences[0].Name).To(Equal(clusterName))
		})

		By("recreating the Lease after it is deleted", func() {
			toDelete := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			}
			Expect(env.Client.Delete(env.Ctx, toDelete)).To(Succeed())

			// The deletion watch triggers a reconcile that recreates the Lease,
			// and the primary re-acquires it.
			Eventually(func(g Gomega) {
				lease := getLease(g)
				g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
				g.Expect(*lease.Spec.HolderIdentity).To(Equal(currentPrimary))
			}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
		})

		By("following the new primary after a switchover", func() {
			clusterasserts.AssertSwitchover(env, testTimeouts, namespace, clusterName)

			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			newPrimary := cluster.Status.CurrentPrimary
			Expect(newPrimary).ToNot(Equal(currentPrimary))

			Eventually(func(g Gomega) {
				lease := getLease(g)
				g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
				g.Expect(*lease.Spec.HolderIdentity).To(Equal(newPrimary))
			}, testTimeouts[timeouts.NewPrimaryAfterSwitchover]).Should(Succeed())
		})
	})
})
