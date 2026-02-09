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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Dynamic Storage Scaling Tests
// These tests validate storage resize operations during scaling and rate-limiting.
var _ = Describe("Dynamic Storage Scaling", Serial, Label(tests.LabelDisruptive, tests.LabelStorage), func() {
	const level = tests.Medium

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("new replica inherits resized PVC size", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should provision new replica with resized PVC size with 2 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-newreplica-t2"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_newrep", 100)
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

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("cleaning up fill files before scaling", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})

			var currentPVCSize resource.Quantity
			By("getting the current PVC size after resize", func() {
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentPVCSize = pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							break
						}
					}
					g.Expect(currentPVCSize.Cmp(originalSize)).To(BeNumerically(">", 0))
				}, 1*time.Minute, 10*time.Second).Should(Succeed())
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

			By("verifying new replica PVC has resized size", func() {
				newPVCName := clusterName + "-3"
				assertNewReplicaPVCSize(namespace, newPVCName, currentPVCSize)
			})

			By("verifying all standbys are streaming", func() {
				assertClusterStandbysAreStreaming(namespace, clusterName, 120)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_newrep", 100)
			})
		})

		It("should provision new replica with resized PVC size with 3 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-newreplica-t3"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile3Inst := fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName3 := "cluster-autoresize-disruption-3inst"
			AssertCreateCluster(namespace, clusterName3, sampleFile3Inst, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName3, "sentinel_newrep3", 100)
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

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName3, originalSize, 25*time.Minute)
			})

			By("cleaning up fill files before scaling", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})

			var currentPVCSize resource.Quantity
			By("getting the current PVC size after resize", func() {
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName3 &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentPVCSize = pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							break
						}
					}
					g.Expect(currentPVCSize.Cmp(originalSize)).To(BeNumerically(">", 0))
				}, 1*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("scaling the cluster from 3 to 4 instances", func() {
				_, _, err = run.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName3))
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for scale-up to complete", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName3)
					return cluster.Status.ReadyInstances, err
				}, testTimeouts[testsUtils.ClusterIsReady]).Should(BeEquivalentTo(4))
			})

			By("verifying new replica PVC has resized size", func() {
				newPVCName := clusterName3 + "-4"
				assertNewReplicaPVCSize(namespace, newPVCName, currentPVCSize)
			})

			By("verifying all standbys are streaming", func() {
				assertClusterStandbysAreStreaming(namespace, clusterName3, 120)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName3, "sentinel_newrep3", 100)
			})
		})
	})

	Context("rate limiting enforcement", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-ratelimit.yaml.template"
			clusterName = "cluster-autoresize-ratelimit"
		)

		It("should block resize when daily limit is reached with 2 instances", func(_ SpecContext) {
			const namespacePrefix = "resize-ratelimit-t2"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_rl", 100)
			})

			primaryPodName := clusterName + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger first auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("waiting for first resize to complete", func() {
				waitForResizeTriggered(namespace, clusterName)
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})

			By("waiting for disk usage to drop", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.DiskStatus).ToNot(BeNil())
					instance, ok := cluster.Status.DiskStatus.Instances[primaryPodName]
					g.Expect(ok).To(BeTrue())
					g.Expect(instance.DataVolume).ToNot(BeNil())
					g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically("<", 80))
				}, 2*time.Minute, 10*time.Second).Should(Succeed())
			})

			var sizeAfterFirstResize resource.Quantity
			By("recording PVC size after first resize", func() {
				pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())
				for idx := range pvcList.Items {
					pvc := &pvcList.Items[idx]
					if pvc.Labels[utils.ClusterLabelName] == clusterName &&
						pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
						sizeAfterFirstResize = pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						break
					}
				}
			})

			By("filling the disk again to attempt second resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 2400)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("verifying second resize is blocked by rate limit", func() {
				// Check for rate limit event
				Eventually(func(g Gomega) {
					events, err := env.Interface.CoreV1().Events(namespace).List(env.Ctx, metav1.ListOptions{})
					g.Expect(err).ToNot(HaveOccurred())

					rateLimited := false
					for _, event := range events.Items {
						if event.InvolvedObject.Kind == "Cluster" &&
							event.InvolvedObject.Name == clusterName {
							if event.Reason == "AutoResizeBlocked" ||
								event.Reason == "AutoResizeRateLimited" ||
								event.Reason == "RateLimitExceeded" {
								rateLimited = true
								GinkgoWriter.Printf("[EVENT] Rate limit hit: %s - %s\n", event.Reason, event.Message)
								break
							}
						}
					}
					g.Expect(rateLimited).To(BeTrue(), "Second resize should be rate-limited")
				}, 3*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("verifying PVC size has not increased beyond first resize", func() {
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							g.Expect(currentSize.Cmp(sizeAfterFirstResize)).To(BeNumerically("<=", 0),
								"PVC should not grow beyond first resize due to rate limit")
						}
					}
				}, 1*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_rl", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})

		It("should block resize when daily limit is reached with single instance", func(_ SpecContext) {
			const namespacePrefix = "resize-ratelimit-t1"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sampleFile1Inst := fixturesDir + "/auto_resize/cluster-autoresize-ratelimit-1inst.yaml.template"
			clusterName1 := "cluster-autoresize-ratelimit-1inst"
			AssertCreateCluster(namespace, clusterName1, sampleFile1Inst, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName1, "sentinel_rl1", 100)
			})

			primaryPodName := clusterName1 + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the disk to trigger first auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName1, primaryPodName, 80)
			})

			By("waiting for first resize to complete", func() {
				waitForResizeTriggered(namespace, clusterName1)
				assertPVCResized(namespace, clusterName1, originalSize, 25*time.Minute)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})

			By("waiting for disk usage to drop", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName1)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.DiskStatus).ToNot(BeNil())
					instance, ok := cluster.Status.DiskStatus.Instances[primaryPodName]
					g.Expect(ok).To(BeTrue())
					g.Expect(instance.DataVolume).ToNot(BeNil())
					g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically("<", 80))
				}, 2*time.Minute, 10*time.Second).Should(Succeed())
			})

			var sizeAfterFirstResize resource.Quantity
			By("recording PVC size after first resize", func() {
				pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())
				for idx := range pvcList.Items {
					pvc := &pvcList.Items[idx]
					if pvc.Labels[utils.ClusterLabelName] == clusterName1 &&
						pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
						sizeAfterFirstResize = pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						break
					}
				}
			})

			By("filling the disk again to attempt second resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 2400)
				assertDiskUsageExceedsThreshold(namespace, clusterName1, primaryPodName, 80)
			})

			By("verifying second resize is blocked by rate limit", func() {
				Eventually(func(g Gomega) {
					events, err := env.Interface.CoreV1().Events(namespace).List(env.Ctx, metav1.ListOptions{})
					g.Expect(err).ToNot(HaveOccurred())

					rateLimited := false
					for _, event := range events.Items {
						if event.InvolvedObject.Kind == "Cluster" &&
							event.InvolvedObject.Name == clusterName1 {
							if event.Reason == "AutoResizeBlocked" ||
								event.Reason == "AutoResizeRateLimited" ||
								event.Reason == "RateLimitExceeded" {
								rateLimited = true
								break
							}
						}
					}
					g.Expect(rateLimited).To(BeTrue(), "Second resize should be rate-limited")
				}, 3*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("verifying PVC size has not increased beyond first resize", func() {
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName1 &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							g.Expect(currentSize.Cmp(sizeAfterFirstResize)).To(BeNumerically("<=", 0))
						}
					}
				}, 1*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName1, "sentinel_rl1", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("policy limit enforcement", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-small-limit.yaml.template"
			clusterName = "cluster-autoresize-small-limit"
		)

		It("should stop resizing at policy limit", func(_ SpecContext) {
			const namespacePrefix = "resize-limit"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_limit", 100)
			})

			primaryPodName := clusterName + "-1"
			limitSize := resource.MustParse("3Gi")

			By("filling the disk to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("waiting for resize attempts and limit enforcement", func() {
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							g.Expect(currentSize.Cmp(limitSize)).To(BeNumerically("<=", 0),
								"PVC size should not exceed limit of %s", limitSize.String())
						}
					}
				}, 10*time.Minute, 15*time.Second).Should(Succeed())
			})

			By("verifying limit is respected over time", func() {
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							g.Expect(currentSize.Cmp(limitSize)).To(BeNumerically("<=", 0),
								"PVC size should never exceed limit")
						}
					}
				}, 1*time.Minute, 15*time.Second).Should(Succeed())
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_limit", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})
})
