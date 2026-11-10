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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Enabling a plugin on a running cluster leaves the existing Pods without the
// sidecar it injects until they are recreated. The primary is rolled out
// last, through a switchover that it can only complete by demoting itself
// while its sidecar is still missing.
var _ = Describe("plugin-barman-cloud sidecar rollout", Label(tests.LabelPluginBarmanCloud), func() {
	const (
		clusterManifest = fixturesDir + "/plugin_barman_cloud/cluster-sidecar-rollout.yaml.template"
		// Matches metadata.name of the cluster fixture
		clusterName           = "pg-sidecar-rollout"
		barmanCloudPluginName = "barman-cloud.cloudnative-pg.io"
		sidecarName           = "plugin-barman-cloud"
		level                 = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	DescribeTable("rolls out the plugin sidecar to every Pod after a plugin is enabled",
		func(updateStrategy apiv1.PrimaryUpdateStrategy) {
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, "plugin-sidecar-rollout")
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, clusterName)
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)

			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			oldPrimary := cluster.Status.CurrentPrimary

			By("enabling plugin-barman-cloud", func() {
				original := cluster.DeepCopy()
				cluster.Spec.PrimaryUpdateStrategy = updateStrategy
				cluster.Spec.Plugins = []apiv1.PluginConfiguration{
					{
						Name:          barmanCloudPluginName,
						IsWALArchiver: ptr.To(false),
						Parameters:    map[string]string{"barmanObjectName": clusterName},
					},
				}
				Expect(objects.Patch(env.Ctx, env.Client, cluster, client.MergeFrom(original))).To(Succeed())
			})

			if updateStrategy == apiv1.PrimaryUpdateStrategySupervised {
				clusterasserts.AssertClusterEventuallyReachesPhase(env, namespace, clusterName,
					[]string{apiv1.PhaseWaitingForUser}, testTimeouts[timeouts.ClusterIsReady])
				clusterasserts.AssertSwitchover(env, testTimeouts, namespace, clusterName)
			}

			By("waiting for a replica to become the primary", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.CurrentPrimary).ToNot(Equal(oldPrimary))
					g.Expect(cluster.Status.CurrentPrimary).To(Equal(cluster.Status.TargetPrimary))
				}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
			})

			By("waiting for every Pod to run the plugin sidecar", func() {
				Eventually(func(g Gomega) {
					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(podList.Items).To(HaveLen(2))
					for _, pod := range podList.Items {
						g.Expect(pod.Spec.InitContainers).To(
							ContainElement(HaveField("Name", sidecarName)), "pod %s", pod.Name)
					}
				}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
			})

			clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
		},
		Entry("when the primary update is unsupervised", apiv1.PrimaryUpdateStrategyUnsupervised),
		Entry("when the user promotes a replica during a supervised update", apiv1.PrimaryUpdateStrategySupervised),
	)
})
