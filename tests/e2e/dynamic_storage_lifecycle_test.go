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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Dynamic Storage Lifecycle Tests
// These tests validate storage resize operations during cluster lifecycle events.
var _ = Describe("Dynamic Storage Lifecycle", Serial, Label(tests.LabelDisruptive, tests.LabelStorage), func() {
	const level = tests.Medium

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("cluster deletion during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should handle cluster deletion during resize gracefully with 2 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-delete-t2"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			primaryPodName := clusterName + "-1"

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("deleting the cluster during resize", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				err = env.Client.Delete(env.Ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying cluster is deleted without hanging", func() {
				Eventually(func() bool {
					_, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return err != nil
				}, 5*time.Minute, 10*time.Second).Should(BeTrue(),
					"Cluster deletion should complete without deadlock")
			})
		})

		It("should handle cluster deletion during resize with single instance", func(_ SpecContext) {
			const namespacePrefix = "resize-delete-t1"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile1Inst := fixturesDir + "/auto_resize/cluster-autoresize-disruption-1inst.yaml.template"
			clusterName1 := "cluster-autoresize-disruption-1inst"
			AssertCreateCluster(namespace, clusterName1, sampleFile1Inst, env)

			primaryPodName := clusterName1 + "-1"

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName1, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName1)
			})

			By("deleting the cluster during resize", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName1)
				Expect(err).ToNot(HaveOccurred())

				err = env.Client.Delete(env.Ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying cluster is deleted without hanging", func() {
				Eventually(func() bool {
					_, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName1)
					return err != nil
				}, 5*time.Minute, 10*time.Second).Should(BeTrue(),
					"Cluster deletion should complete without deadlock")
			})
		})
	})

	Context("cluster hibernation during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should preserve resize state across hibernation", func(_ SpecContext) {
			const namespacePrefix = "resize-hibernate"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_hibernate", 100)
			})

			primaryPodName := clusterName + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("hibernating the cluster", func() {
				Eventually(func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}

					updatedCluster := cluster.DeepCopy()
					if updatedCluster.Annotations == nil {
						updatedCluster.Annotations = make(map[string]string)
					}
					updatedCluster.Annotations["cnpg.io/hibernation"] = "on"

					return env.Client.Patch(env.Ctx, updatedCluster, ctrlclient.MergeFrom(cluster))
				}, 30*time.Second, 5*time.Second).Should(Succeed())
			})

			By("waiting for cluster to hibernate", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return cluster.Status.ReadyInstances, err
				}, testTimeouts[testsUtils.ClusterIsReady]).Should(BeEquivalentTo(0))
			})

			By("waking the cluster from hibernation", func() {
				Eventually(func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}

					updatedCluster := cluster.DeepCopy()
					delete(updatedCluster.Annotations, "cnpg.io/hibernation")

					return env.Client.Patch(env.Ctx, updatedCluster, ctrlclient.MergeFrom(cluster))
				}, 30*time.Second, 5*time.Second).Should(Succeed())
			})

			By("waiting for cluster to become ready", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying resize completes after wake", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying data integrity after hibernation", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_hibernate", 100)
			})

			By("cleaning up fill files", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				cleanupFillFiles(namespace, primary.Name)
			})
		})
	})

	Context("namespace deletion during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should handle namespace deletion during resize gracefully", func(_ SpecContext) {
			const namespacePrefix = "resize-ns-delete"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			primaryPodName := clusterName + "-1"

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("deleting the namespace during resize", func() {
				err = namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, namespace, 300)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying namespace deletion completes without hanging", func() {
				Eventually(func() bool {
					ns := &corev1.Namespace{}
					getErr := env.Client.Get(env.Ctx, types.NamespacedName{Name: namespace}, ns)
					return getErr != nil
				}, 5*time.Minute, 10*time.Second).Should(BeTrue(),
					"Namespace deletion should complete without deadlock")
			})
		})
	})
})
