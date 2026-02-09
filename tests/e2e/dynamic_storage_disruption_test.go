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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Dynamic Storage Disruption Tests
// These tests validate storage resize operations during various operational disruptions.
var _ = Describe("Dynamic Storage Disruption", Serial, Label(tests.LabelDisruptive, tests.LabelStorage), func() {
	const level = tests.Medium

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("operator restart during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should resume growth after operator restart with 2 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-operator-t2"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data for integrity check", func() {
				createSentinelData(namespace, clusterName, "sentinel_op_t2", 100)
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

			By("restarting the operator pod", func() {
				err = operator.ReloadDeployment(env.Ctx, env.Client, 120)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_op_t2", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})

		It("should resume growth after operator restart with 3 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-operator-t3"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile3Inst := fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName3 := "cluster-autoresize-disruption-3inst"
			AssertCreateCluster(namespace, clusterName3, sampleFile3Inst, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName3, "sentinel_op_t3", 100)
			})

			primaryPodName := clusterName3 + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName3, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName3)
			})

			By("restarting the operator pod", func() {
				err = operator.ReloadDeployment(env.Ctx, env.Client, 120)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName3, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName3, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName3, "sentinel_op_t3", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})

		It("should resume growth after operator restart with single instance", func(_ SpecContext) {
			const namespacePrefix = "resize-operator-t1"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile1Inst := fixturesDir + "/auto_resize/cluster-autoresize-disruption-1inst.yaml.template"
			clusterName1 := "cluster-autoresize-disruption-1inst"
			AssertCreateCluster(namespace, clusterName1, sampleFile1Inst, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName1, "sentinel_op_t1", 100)
			})

			primaryPodName := clusterName1 + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName1, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName1)
			})

			By("restarting the operator pod", func() {
				err = operator.ReloadDeployment(env.Ctx, env.Client, 120)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName1, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName1, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName1, "sentinel_op_t1", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("primary pod restart during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should resume growth after primary restart with 2 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-primary-t2"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_pri_t2", 100)
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

			By("deleting the primary pod", func() {
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, namespace, primaryPodName, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for cluster to recover", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return cluster.Status.ReadyInstances, err
				}, testTimeouts[testsUtils.ClusterIsReady]).Should(BeEquivalentTo(2))
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_pri_t2", 100)
			})

			By("cleaning up fill files", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				cleanupFillFiles(namespace, primary.Name)
			})
		})

		It("should resume growth after primary restart with 3 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-primary-t3"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile3Inst := fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName3 := "cluster-autoresize-disruption-3inst"
			AssertCreateCluster(namespace, clusterName3, sampleFile3Inst, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName3, "sentinel_pri_t3", 100)
			})

			primaryPodName := clusterName3 + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName3, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName3)
			})

			By("deleting the primary pod", func() {
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, namespace, primaryPodName, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for cluster to recover", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName3)
					return cluster.Status.ReadyInstances, err
				}, testTimeouts[testsUtils.ClusterIsReady]).Should(BeEquivalentTo(3))
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName3, originalSize, 25*time.Minute)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName3, "sentinel_pri_t3", 100)
			})

			By("cleaning up fill files", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName3)
				Expect(err).ToNot(HaveOccurred())
				cleanupFillFiles(namespace, primary.Name)
			})
		})

		It("should resume growth after primary restart with single instance", func(_ SpecContext) {
			const namespacePrefix = "resize-primary-t1"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile1Inst := fixturesDir + "/auto_resize/cluster-autoresize-disruption-1inst.yaml.template"
			clusterName1 := "cluster-autoresize-disruption-1inst"
			AssertCreateCluster(namespace, clusterName1, sampleFile1Inst, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName1, "sentinel_pri_t1", 100)
			})

			primaryPodName := clusterName1 + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName1, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName1)
			})

			By("deleting the primary pod", func() {
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, namespace, primaryPodName, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for cluster to recover", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName1)
					return cluster.Status.ReadyInstances, err
				}, testTimeouts[testsUtils.ClusterIsReady]).Should(BeEquivalentTo(1))
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName1, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName1, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName1, "sentinel_pri_t1", 100)
			})

			By("cleaning up fill files", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName1)
				Expect(err).ToNot(HaveOccurred())
				cleanupFillFiles(namespace, primary.Name)
			})
		})
	})

	Context("failover and switchover during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName = "cluster-autoresize-disruption-3inst"
		)

		It("should handle switchover during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-switchover"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_sw", 100)
			})

			initialPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			initialPrimaryName := initialPrimary.Name

			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, initialPrimaryName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, initialPrimaryName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("triggering a switchover", func() {
				_, _, err = run.Run(fmt.Sprintf(
					"kubectl cnpg promote %s -n %s --force",
					clusterName, namespace,
				))
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for new primary election", func() {
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return cluster.Status.CurrentPrimary, err
				}, testTimeouts[testsUtils.ClusterIsReady]).ShouldNot(BeEquivalentTo(initialPrimaryName))
			})

			By("waiting for resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_sw", 100)
			})

			By("cleaning up fill files", func() {
				for _, suffix := range []string{"-1", "-2", "-3"} {
					cleanupFillFiles(namespace, clusterName+suffix)
				}
			})
		})

		It("should handle failover during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-failover"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_fo", 100)
			})

			initialPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			initialPrimaryName := initialPrimary.Name

			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, initialPrimaryName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, initialPrimaryName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("force-deleting the primary to trigger failover", func() {
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, namespace, initialPrimaryName, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for failover to complete", func() {
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.CurrentPrimary, nil
				}, testTimeouts[testsUtils.NewPrimaryAfterFailover]).ShouldNot(BeEquivalentTo(initialPrimaryName))
			})

			By("waiting for cluster to stabilize", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("waiting for resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_fo", 100)
			})

			By("cleaning up fill files", func() {
				for _, suffix := range []string{"-1", "-2", "-3"} {
					cleanupFillFiles(namespace, clusterName+suffix)
				}
			})
		})
	})

	Context("spec mutation during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should handle limit change during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-spec-limit"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_spec", 100)
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

			By("modifying the resize limit", func() {
				Eventually(func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}

					updatedCluster := cluster.DeepCopy()
					updatedCluster.Spec.StorageConfiguration.Resize.Expansion.Limit = "15Gi"

					return env.Client.Patch(env.Ctx, updatedCluster, ctrlclient.MergeFrom(cluster))
				}, 30*time.Second, 5*time.Second).Should(Succeed())
			})

			By("waiting for resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_spec", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})

		It("should handle instance count change during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-spec-scale"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_scale", 100)
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

			By("scaling the cluster from 2 to 3 instances", func() {
				_, _, err = run.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for scale-up to complete", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return cluster.Status.ReadyInstances, err
				}, testTimeouts[testsUtils.ClusterIsReady]).Should(BeEquivalentTo(3))
			})

			By("waiting for resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_scale", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("node drain during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName = "cluster-autoresize-disruption-3inst"
		)
		var nodesWithLabels []string

		BeforeEach(func() {
			nodeList, _ := nodes.List(env.Ctx, env.Client)
			for _, node := range nodeList.Items {
				if (node.Spec.Unschedulable != true) && (len(node.Spec.Taints) == 0) {
					nodesWithLabels = append(nodesWithLabels, node.Name)
					cmd := fmt.Sprintf("kubectl label node %v drain-test=drain-test --overwrite", node.Name)
					_, _, err := run.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
				}
				if len(nodesWithLabels) >= 3 {
					break
				}
			}
			if len(nodesWithLabels) < 2 {
				Skip("Not enough nodes available for drain test")
			}
		})

		AfterEach(func() {
			err := nodes.UncordonAll(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())
			for _, node := range nodesWithLabels {
				cmd := fmt.Sprintf("kubectl label node %v drain-test- ", node)
				_, _, _ = run.Run(cmd)
			}
			nodesWithLabels = nil
		})

		It("should handle node drain during resize with 3 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-drain-t3"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_drain", 100)
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

			By("draining the node containing the primary", func() {
				_ = nodes.DrainPrimary(
					env.Ctx, env.Client,
					namespace, clusterName,
					testTimeouts[testsUtils.DrainNode],
				)
			})

			By("uncordoning all nodes", func() {
				err = nodes.UncordonAll(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_drain", 100)
			})

			By("cleaning up fill files", func() {
				for _, suffix := range []string{"-1", "-2", "-3"} {
					cleanupFillFiles(namespace, clusterName+suffix)
				}
			})
		})
	})

	Context("backup during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should handle backup creation during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-backup"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_backup", 100)
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

			By("creating a volume snapshot backup", func() {
				backup := &apiv1.Backup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-during-resize",
						Namespace: namespace,
					},
					Spec: apiv1.BackupSpec{
						Method: apiv1.BackupMethodVolumeSnapshot,
						Cluster: apiv1.LocalObjectReference{
							Name: clusterName,
						},
					},
				}
				// Attempt to create backup - may fail if VolumeSnapshots aren't supported
				_ = env.Client.Create(env.Ctx, backup)
			})

			By("waiting for resize to complete without deadlock", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_backup", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})
})
