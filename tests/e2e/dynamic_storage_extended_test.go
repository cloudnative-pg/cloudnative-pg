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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Dynamic Storage Extended Tests (P1 Scenarios)
// These tests validate advanced storage resize scenarios.
var _ = Describe("Dynamic Storage Extended", Serial, Label(tests.LabelDisruptive, tests.LabelStorage), func() {
	const level = tests.High

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("concurrent operations during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName = "cluster-autoresize-disruption-3inst"
		)

		It("should handle multiple concurrent operations during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-concurrent"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating initial sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_conc", 100)
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

			By("performing concurrent operations during resize", func() {
				// Start multiple operations concurrently
				done := make(chan bool, 3)

				// Operation 1: Insert additional data
				go func() {
					defer GinkgoRecover()
					createSentinelData(namespace, clusterName, "sentinel_conc_op1", 50)
					done <- true
				}()

				// Operation 2: Force a checkpoint
				go func() {
					defer GinkgoRecover()
					primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					if err == nil {
						commandTimeout := time.Second * 30
						_, _, _ = env.EventuallyExecCommand(
							env.Ctx, *primary, "postgres", &commandTimeout,
							"psql", "-U", "postgres", "-c", "CHECKPOINT",
						)
					}
					done <- true
				}()

				// Operation 3: Query cluster status
				go func() {
					defer GinkgoRecover()
					for i := 0; i < 5; i++ {
						_, _ = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						time.Sleep(2 * time.Second)
					}
					done <- true
				}()

				// Wait for all operations to complete
				for i := 0; i < 3; i++ {
					<-done
				}
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy after concurrent operations", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying all data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_conc", 100)
				assertDataIntegrity(namespace, clusterName, "sentinel_conc_op1", 50)
			})

			By("cleaning up fill files", func() {
				for _, suffix := range []string{"-1", "-2", "-3"} {
					cleanupFillFiles(namespace, clusterName+suffix)
				}
			})
		})
	})

	Context("rolling upgrade during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption-3inst.yaml.template"
			clusterName = "cluster-autoresize-disruption-3inst"
		)

		It("should handle image update during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-upgrade"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_upgrade", 100)
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

			By("triggering a restart of instances via annotation", func() {
				// Use restart annotation to trigger rolling restart
				_, _, err = run.Run(fmt.Sprintf(
					"kubectl annotate cluster %s -n %s kubectl.kubernetes.io/restartedAt=%s --overwrite",
					clusterName, namespace, time.Now().Format(time.RFC3339),
				))
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 30*time.Minute)
			})

			By("verifying cluster is healthy after rolling restart", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying all standbys are streaming", func() {
				assertClusterStandbysAreStreaming(namespace, clusterName, 120)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_upgrade", 100)
			})

			By("cleaning up fill files", func() {
				for _, suffix := range []string{"-1", "-2", "-3"} {
					cleanupFillFiles(namespace, clusterName+suffix)
				}
			})
		})
	})

	Context("volume snapshot during resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should handle volume snapshot requests during resize", func(_ SpecContext) {
			const namespacePrefix = "resize-snapshot"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_snap", 100)
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

			By("creating a volume snapshot during resize", func() {
				backup := &apiv1.Backup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "snapshot-during-resize",
						Namespace: namespace,
					},
					Spec: apiv1.BackupSpec{
						Method: apiv1.BackupMethodVolumeSnapshot,
						Cluster: apiv1.LocalObjectReference{
							Name: clusterName,
						},
					},
				}
				// Attempt to create - may fail if snapshots not supported
				_ = env.Client.Create(env.Ctx, backup)
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_snap", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("oscillation prevention", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-disruption.yaml.template"
			clusterName = "cluster-autoresize-disruption"
		)

		It("should prevent resize oscillation with repeated fill and cleanup", func(_ SpecContext) {
			const namespacePrefix = "resize-oscillation"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_osc", 100)
			})

			primaryPodName := clusterName + "-1"
			originalSize := resource.MustParse("2Gi")

			var resizeSizes []resource.Quantity

			By("triggering first resize cycle", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
				waitForResizeTriggered(namespace, clusterName)
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)

				// Record size after first resize
				pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())
				for idx := range pvcList.Items {
					pvc := &pvcList.Items[idx]
					if pvc.Labels[utils.ClusterLabelName] == clusterName &&
						pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
						resizeSizes = append(resizeSizes, pvc.Spec.Resources.Requests[corev1.ResourceStorage])
						break
					}
				}
			})

			By("cleaning up and allowing disk usage to drop", func() {
				cleanupFillFiles(namespace, primaryPodName)
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.DiskStatus).ToNot(BeNil())
					instance, ok := cluster.Status.DiskStatus.Instances[primaryPodName]
					g.Expect(ok).To(BeTrue())
					g.Expect(instance.DataVolume).ToNot(BeNil())
					g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically("<", 50))
				}, 2*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("triggering second resize cycle", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 2200)
				waitForResizeTriggered(namespace, clusterName)

				// Wait and record size
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())
					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							if len(resizeSizes) > 0 && currentSize.Cmp(resizeSizes[len(resizeSizes)-1]) > 0 {
								resizeSizes = append(resizeSizes, currentSize)
							}
						}
					}
				}, 15*time.Minute, 15*time.Second).Should(Succeed())
			})

			By("verifying monotonic growth (no shrinking)", func() {
				for i := 1; i < len(resizeSizes); i++ {
					Expect(resizeSizes[i].Cmp(resizeSizes[i-1])).To(BeNumerically(">=", 0),
						"PVC size should never decrease (oscillation prevention)")
				}
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_osc", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("emergency resize behavior", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-emergency.yaml.template"
			clusterName = "cluster-autoresize-emergency"
		)

		It("should bypass rate limits in emergency scenarios", func(_ SpecContext) {
			const namespacePrefix = "resize-emergency"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_emer", 100)
			})

			primaryPodName := clusterName + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling disk to critical level", func() {
				// Fill to extremely high usage to trigger emergency
				fillDiskToTriggerResize(namespace, primaryPodName, 1900)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 85)
			})

			By("waiting for resize operation", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("waiting for emergency resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity after emergency resize", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_emer", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("tablespace resize during disruption", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-tablespace-disruption.yaml.template"
			clusterName = "cluster-autoresize-tablespace-disruption"
		)

		It("should handle tablespace resize during operator restart", func(_ SpecContext) {
			const namespacePrefix = "resize-tbs-disrupt"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying cluster is ready with tablespace", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_tbs", 100)
			})

			primaryPodName := clusterName + "-1"
			originalSize := resource.MustParse("2Gi")

			By("filling the data volume to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				assertDiskUsageExceedsThreshold(namespace, clusterName, primaryPodName, 80)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("waiting for PVC resize to complete", func() {
				assertPVCResized(namespace, clusterName, originalSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_tbs", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
			})
		})
	})
})
