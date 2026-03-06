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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBouncer Pause During Switchover", Label(tests.LabelServiceConnectivity), func() {
	const (
		clusterSampleFile = fixturesDir + "/pgbouncer/cluster-pgbouncer-pause-switchover.yaml.template"
		poolerSampleFile  = fixturesDir + "/pgbouncer/pooler-pause-switchover-rw.yaml"
		level             = tests.Low
	)
	var namespace, clusterName string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("automatic pause/resume during switchover", Ordered, func() {
		BeforeAll(func() {
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "pgbouncer-pause-switchover")
			Expect(err).ToNot(HaveOccurred())
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterSampleFile)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)
		})

		It("pauses pooler during switchover and resumes after", func() {
			poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("creating a pooler with pauseDuringSwitchover enabled", func() {
				createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerSampleFile, 1)
			})

			By("triggering a switchover", func() {
				AssertSwitchover(namespace, clusterName, env)
			})

			By("verifying the pooler resumes after switchover completion", func() {
				// After switchover completes, the pooler should be resumed
				Eventually(func(g Gomega) {
					pooler := &apiv1.Pooler{}
					err := env.Client.Get(env.Ctx,
						types.NamespacedName{Name: poolerName, Namespace: namespace}, pooler)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(pooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
					g.Expect(pooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
					g.Expect(pooler.Status.PausedForSwitchover).To(BeFalse())
				}, 120).Should(Succeed())
			})
		})

		It("does not affect manually paused poolers", func() {
			poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("manually pausing the pooler", func() {
				Eventually(func(g Gomega) {
					pooler := &apiv1.Pooler{}
					err := env.Client.Get(env.Ctx,
						types.NamespacedName{Name: poolerName, Namespace: namespace}, pooler)
					g.Expect(err).ToNot(HaveOccurred())

					origPooler := pooler.DeepCopy()
					pooler.Spec.PgBouncer.Paused = ptr.To(true)
					g.Expect(env.Client.Patch(env.Ctx, pooler, ctrlclient.MergeFrom(origPooler))).To(Succeed())
				}, 30).Should(Succeed())
			})

			By("triggering a switchover", func() {
				AssertSwitchover(namespace, clusterName, env)
			})

			By("verifying the pooler remains manually paused (no annotation added)", func() {
				Consistently(func(g Gomega) {
					pooler := &apiv1.Pooler{}
					err := env.Client.Get(env.Ctx,
						types.NamespacedName{Name: poolerName, Namespace: namespace}, pooler)
					g.Expect(err).ToNot(HaveOccurred())
					// Should still be paused but without our annotation
					g.Expect(pooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
					g.Expect(pooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
				}, 30).Should(Succeed())
			})

			By("unpausing the pooler for cleanup", func() {
				Eventually(func(g Gomega) {
					pooler := &apiv1.Pooler{}
					err := env.Client.Get(env.Ctx,
						types.NamespacedName{Name: poolerName, Namespace: namespace}, pooler)
					g.Expect(err).ToNot(HaveOccurred())

					origPooler := pooler.DeepCopy()
					pooler.Spec.PgBouncer.Paused = ptr.To(false)
					g.Expect(env.Client.Patch(env.Ctx, pooler, ctrlclient.MergeFrom(origPooler))).To(Succeed())
				}, 30).Should(Succeed())
			})
		})
	})
})
