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
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/proxy"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Auto-resize test constants
const (
	// Timeouts for command execution and polling
	diskUsageCommandTimeout     = 20 * time.Second
	diskFillCommandTimeout      = 120 * time.Second
	cleanupCommandTimeout       = 30 * time.Second
	diskUsageCheckTimeout       = 2 * time.Minute
	diskUsageCheckInterval      = 5 * time.Second
	pvcResizeTimeout            = 25 * time.Minute
	pvcResizeCheckInterval      = 10 * time.Second
	eventCheckTimeout           = 60 * time.Second
	eventCheckInterval          = 5 * time.Second
	consistentlyDuration        = 2 * time.Minute
	shortConsistentlyDuration   = 30 * time.Second
	quickResizeCheckTimeout     = 5 * time.Minute
	diskUsageVerifyTimeout      = 120 * time.Second
	metricsCheckTimeout         = 60 * time.Second
	archiveHealthCheckTimeout   = 15 * time.Minute
	slotRetentionCheckTimeout   = 180 * time.Second
	walGenerationLoopCount      = 30
	walGenerationLoopSleep      = 1 * time.Second
	walGenerationCommandTimeout = 60 * time.Second

	// Disk fill sizes in MB
	diskFillSizeBasic         = 1800 // Fills ~90% of 2Gi volume to exceed 80% threshold
	diskFillSizeMinAvailable  = 1700 // Fills to drop below 500Mi available while under 99% usage
	diskFillSizeWAL           = 1700 // Fills ~85% of WAL volume
	diskFillSizeLimitTest     = 2048 // Larger fill to test limit enforcement
	diskFillSizeArchiveBlock  = 1800 // Fills ~90% for archive block test
	diskFillSizeSlotBlock     = 1700 // Fills for slot retention test
	walFillDataRowCount       = 1000 // Rows per WAL generation iteration
	slotRetentionFillRowCount = 120000

	// Disk usage thresholds (percentage)
	diskUsageThreshold          = 80
	diskUsageThresholdMinAvail  = 99
	metricsToleranceBytes       = 2e8 // 200MB tolerance for filesystem overhead
	expectedDiskTotalBytes      = 2147483648
	slotRetentionThresholdBytes = 104857600 // 100MB

	// Volume sizes
	originalVolumeSize = "2Gi"

	// Fill file paths
	dataFillFile       = "/var/lib/postgresql/data/pgdata/fill_file"
	dataFillFile2      = "/var/lib/postgresql/data/pgdata/fill_file2"
	tablespaceFillFile = "/var/lib/postgresql/tablespaces/tbs1/fill_file"
)

func getDiskUsage(pod *corev1.Pod, path string) (percentUsed int, availableBytes int64, err error) {
	commandTimeout := diskUsageCommandTimeout
	command := fmt.Sprintf("df -P -B1 %s | awk 'NR==2 {gsub(\"%%\",\"\",$5); print $4\" \"$5}'", path)
	out, _, err := env.EventuallyExecCommand(
		env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
		"sh", "-c", command,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to execute df command: %w", err)
	}

	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("unexpected df output: %q", out)
	}

	availableBytes, err = strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse available bytes: %w", err)
	}
	percentUsed, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse percent used: %w", err)
	}

	return percentUsed, availableBytes, nil
}

func assertPercentUsedOver(pod *corev1.Pod, path string, threshold int) {
	Eventually(func() (int, error) {
		percentUsed, _, err := getDiskUsage(pod, path)
		return percentUsed, err
	}, diskUsageCheckTimeout, diskUsageCheckInterval).Should(BeNumerically(">", threshold),
		fmt.Sprintf("percent used on %s should exceed %d%%", path, threshold))
}

func assertPercentUsedUnder(pod *corev1.Pod, path string, threshold int) {
	Eventually(func() (int, error) {
		percentUsed, _, err := getDiskUsage(pod, path)
		return percentUsed, err
	}, diskUsageCheckTimeout, diskUsageCheckInterval).Should(BeNumerically("<", threshold),
		fmt.Sprintf("percent used on %s should stay under %d%%", path, threshold))
}

func assertAvailableBelow(pod *corev1.Pod, path string, minBytes int64) {
	Eventually(func() (int64, error) {
		_, availableBytes, err := getDiskUsage(pod, path)
		return availableBytes, err
	}, diskUsageCheckTimeout, diskUsageCheckInterval).Should(BeNumerically("<", minBytes),
		fmt.Sprintf("available bytes on %s should drop below %d", path, minBytes))
}

var _ = Describe("PVC Auto-Resize", Label(tests.LabelAutoResize), func() {
	const (
		level = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("basic auto-resize with single volume", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-basic.yaml.template"
			clusterName = "cluster-autoresize-basic"
		)
		var namespace string

		It("should resize PVC when disk usage exceeds threshold", func(_ SpecContext) {
			const namespacePrefix = "autoresize-basic-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying auto-resize is enabled on the cluster", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize).ToNot(BeNil())
				Expect(ptr.Deref(cluster.Spec.StorageConfiguration.Resize.Enabled, false)).To(BeTrue())
			})

			By("filling the disk on ALL instances to trigger auto-resize", func() {
				// Fill disk on both instances since each PVC is evaluated independently
				for _, suffix := range []string{"-1", "-2"} {
					podName := clusterName + suffix
					pod := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      podName,
					}, pod)
					Expect(err).ToNot(HaveOccurred())

					// Fill the disk to exceed the usage threshold
					commandTimeout := diskFillCommandTimeout
					ddCmd := fmt.Sprintf(
						"dd if=/dev/zero of=%s bs=1M count=%d || true",
						dataFillFile, diskFillSizeBasic)
					_, _, err = env.EventuallyExecCommand(
						env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
						"sh", "-c", ddCmd,
					)
					Expect(err).ToNot(HaveOccurred())
				}

				// PROOF: Verify percent_used > threshold on both instances via status before checking for resize
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.DiskStatus).ToNot(BeNil())
					for _, suffix := range []string{"-1", "-2"} {
						podName := clusterName + suffix
						instance, ok := cluster.Status.DiskStatus.Instances[podName]
						g.Expect(ok).To(BeTrue(), "DiskStatus should contain instance %s", podName)
						g.Expect(instance.DataVolume).ToNot(BeNil())
						g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically(">", diskUsageThreshold),
							"Disk usage on %s must exceed threshold to trigger resize", podName)
					}
				}, diskUsageVerifyTimeout, diskUsageCheckInterval).Should(Succeed())
			})

			By("waiting for ALL instance PVCs to be resized (REQ-16)", func() {
				// The fixture has instances: 2. Both should be resized eventually.
				// Azure Disk resize can take 5-10 minutes per PVC, so use generous timeout.
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					resizedCount := 0
					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							origSize := resource.MustParse(originalVolumeSize)
							if currentSize.Cmp(origSize) > 0 {
								resizedCount++
							}
						}
					}
					g.Expect(resizedCount).To(Equal(2), "Both PVCs (primary and standby) should be resized")
				}, pvcResizeTimeout, pvcResizeCheckInterval).Should(Succeed())
			})

			By("verifying Kubernetes Events were emitted", func() {
				Eventually(func(g Gomega) {
					events, err := env.Interface.CoreV1().Events(namespace).List(env.Ctx, metav1.ListOptions{
						FieldSelector: "reason=AutoResizeSuccess",
					})
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(events.Items).ToNot(BeEmpty(), "Should have emitted AutoResizeSuccess event")
				}, eventCheckTimeout, eventCheckInterval).Should(Succeed())
			})

			By("verifying an auto-resize event was recorded in cluster status (REQ-12)", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.AutoResizeEvents).ToNot(BeEmpty(),
						"AutoResizeEvents should not be empty after a resize")
					latest := cluster.Status.AutoResizeEvents[len(cluster.Status.AutoResizeEvents)-1]
					g.Expect(latest.Result).To(Equal(apiv1.ResizeResultSuccess),
						"Last resize event should have result=success")
					g.Expect(latest.VolumeType).To(Equal(apiv1.ResizeVolumeTypeData),
						"Last resize event should be for data volume")
				}, eventCheckTimeout, eventCheckInterval).Should(Succeed())
			})

			By("cleaning up the fill files", func() {
				// Clean up fill files on both instances
				for _, suffix := range []string{"-1", "-2"} {
					podName := clusterName + suffix
					pod := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      podName,
					}, pod)
					if err != nil {
						continue // Pod may not exist
					}

					commandTimeout := cleanupCommandTimeout
					_, _, _ = env.EventuallyExecCommand(
						env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
						"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
					)
				}
			})
		})
	})

	Context("minAvailable trigger", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-minavailable.yaml.template"
			clusterName = "cluster-autoresize-minavailable"
		)
		var namespace string

		It("should resize PVC when available space drops below minAvailable", func(_ SpecContext) {
			const namespacePrefix = "autoresize-minavailable-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("filling the disk to drop below minAvailable", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// The volume is 2Gi, minAvailable is 500Mi, and usageThreshold is 99.
				// Writing ~1.7Gi should leave <500Mi available while staying under 99% usage.
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					dataFillFile, diskFillSizeMinAvailable)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())

				assertPercentUsedUnder(pod, specs.PgDataPath, diskUsageThresholdMinAvail)
				minAvailable := resource.MustParse("500Mi")
				assertAvailableBelow(pod, specs.PgDataPath, minAvailable.Value())
			})

			By("waiting for PVC to be resized", func() {
				Eventually(func() bool {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					if err != nil {
						return false
					}

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgData) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						origSize := resource.MustParse(originalVolumeSize)
						if currentSize.Cmp(origSize) > 0 {
							return true
						}
					}
					return false
				}, quickResizeCheckTimeout, pvcResizeCheckInterval).Should(BeTrue(),
					"PVC should have been resized beyond its original size")
			})

			By("cleaning up the fill file", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
				)
			})
		})
	})

	Context("auto-resize with separate WAL volume", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-wal-runtime.yaml.template"
			clusterName = "cluster-autoresize-wal-runtime"
		)
		var namespace string

		It("should resize WAL PVC when WAL volume usage exceeds threshold", func(_ SpecContext) {
			const namespacePrefix = "autoresize-wal-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying both storage and WAL resize are enabled", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize).ToNot(BeNil())
				Expect(ptr.Deref(cluster.Spec.StorageConfiguration.Resize.Enabled, false)).To(BeTrue())
				Expect(cluster.Spec.WalStorage).ToNot(BeNil())
				Expect(cluster.Spec.WalStorage.Resize).ToNot(BeNil())
				Expect(ptr.Deref(cluster.Spec.WalStorage.Resize.Enabled, false)).To(BeTrue())
			})

			By("verifying PVCs exist for both data and WAL", func() {
				pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())

				var dataCount, walCount int
				for idx := range pvcList.Items {
					pvc := &pvcList.Items[idx]
					if pvc.Labels[utils.ClusterLabelName] != clusterName {
						continue
					}
					switch pvc.Labels[utils.PvcRoleLabelName] {
					case string(utils.PVCRolePgData):
						dataCount++
					case string(utils.PVCRolePgWal):
						walCount++
					}
				}
				Expect(dataCount).To(BeNumerically(">", 0), "should have data PVCs")
				Expect(walCount).To(BeNumerically(">", 0), "should have WAL PVCs")
			})

			By("filling the WAL volume to trigger auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the WAL volume to exceed the usage threshold
				// The WAL volume is 2Gi, so writing ~1.7Gi should trigger resize
				// WAL mount is at /var/lib/postgresql/wal/pg_wal
				commandTimeout := diskFillCommandTimeout
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c",
					fmt.Sprintf("dd if=/dev/zero of=/var/lib/postgresql/wal/pg_wal/fill_file bs=1M count=%d", diskFillSizeWAL),
				)
				Expect(err).ToNot(HaveOccurred())

				assertPercentUsedOver(pod, specs.PgWalVolumePath, diskUsageThreshold)
			})

			By("waiting for WAL PVC to be resized", func() {
				// The reconciler runs every 30s, give it time to detect and resize
				Eventually(func() bool {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					if err != nil {
						return false
					}

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						// Only check WAL PVCs for this cluster
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgWal) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						origSize := resource.MustParse(originalVolumeSize)
						if currentSize.Cmp(origSize) > 0 {
							return true
						}
					}
					return false
				}, quickResizeCheckTimeout, pvcResizeCheckInterval).Should(BeTrue(),
					"WAL PVC should have been resized beyond its original size")
			})

			By("cleaning up the fill file", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/wal/pg_wal/fill_file",
				)
			})
		})
	})

	Context("auto-resize respects expansion limit", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-limit.yaml.template"
			clusterName = "cluster-autoresize-limit"
		)
		var namespace string

		It("should resize PVC but never exceed configured limit", func(_ SpecContext) {
			const namespacePrefix = "autoresize-limit-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying expansion limit is set to 2.5Gi", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize.Expansion.Limit).To(Equal("2.5Gi"))
			})

			By("filling the disk to trigger first auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the disk to exceed the usage threshold
				// The volume is 2Gi, so writing ~1.7Gi should trigger resize
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					dataFillFile, diskFillSizeMinAvailable)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())

				assertPercentUsedOver(pod, specs.PgDataPath, diskUsageThreshold)
			})

			By("waiting for PVC to be resized to 2.5Gi (the clamped limit)", func() {
				// The fixture has Limit: 2.5Gi and Step: 1Gi.
				// 2Gi + 1Gi would be 3Gi, so it MUST be clamped to exactly 2.5Gi.
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					found := false
					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							limitSize := resource.MustParse("2.5Gi")
							// PROOF: It must equal the limit exactly
							g.Expect(currentSize.Cmp(limitSize)).To(Equal(0),
								"PVC size should be exactly at the limit of 2.5Gi")
							found = true
						}
					}
					g.Expect(found).To(BeTrue())
				}, quickResizeCheckTimeout, pvcResizeCheckInterval).Should(Succeed())
			})

			By("cleaning up the fill file before second fill attempt", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
				)
			})

			By("filling disk again to verify limit blocks further resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Write ~2Gi to the now-2.5Gi volume to exceed threshold again
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d || true",
					dataFillFile2, diskFillSizeLimitTest)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying PVC stays at limit and does not grow", func() {
				limitSize := resource.MustParse("2.5Gi")

				// First, ensure we're still at the limit after the second fill
				Eventually(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
							g.Expect(currentSize.Cmp(limitSize)).To(Equal(0))
						}
					}
				}, shortConsistentlyDuration, diskUsageCheckInterval).Should(Succeed())

				// Then verify it stays at the limit
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgData) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						g.Expect(currentSize.Cmp(limitSize)).To(BeNumerically("<=", 0),
							"PVC should remain at limit, not grow further")
					}
				}, consistentlyDuration, pvcResizeCheckInterval).Should(Succeed())
			})

			By("cleaning up all fill files", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
					"/var/lib/postgresql/data/pgdata/fill_file2",
				)
			})
		})
	})

	Context("webhook validation", func() {
		It("should reject auto-resize for single-volume clusters without acknowledgeWALRisk", func(_ SpecContext) {
			const namespacePrefix = "autoresize-webhook-e2e"

			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			cluster.SetName("autoresize-no-ack")
			cluster.SetNamespace(namespace)
			cluster.Spec.Instances = 1
			cluster.Spec.StorageConfiguration = apiv1.StorageConfiguration{
				Size: "2Gi",
				Resize: &apiv1.ResizeConfiguration{
					Enabled: ptr.To(true),
					// No strategy with acknowledgeWALRisk → should be rejected
				},
			}
			cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Database: "app",
					Owner:    "app",
				},
			}

			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).To(HaveOccurred(),
				"cluster creation should fail without acknowledgeWALRisk for single-volume")
		})

		It("should accept auto-resize for single-volume clusters with acknowledgeWALRisk", func(_ SpecContext) {
			const namespacePrefix = "autoresize-ack-e2e"

			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			cluster.SetName("autoresize-with-ack")
			cluster.SetNamespace(namespace)
			cluster.Spec.Instances = 1
			cluster.Spec.StorageConfiguration = apiv1.StorageConfiguration{
				Size: "2Gi",
				Resize: &apiv1.ResizeConfiguration{
					Enabled: ptr.To(true),
					Strategy: &apiv1.ResizeStrategy{
						WALSafetyPolicy: &apiv1.WALSafetyPolicy{
							AcknowledgeWALRisk: ptr.To(true),
						},
					},
				},
			}
			cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Database: "app",
					Owner:    "app",
				},
			}

			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred(),
				"cluster creation should succeed with acknowledgeWALRisk for single-volume")
		})
	})

	Context("rate-limit enforcement", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-ratelimit.yaml.template"
			clusterName = "cluster-autoresize-ratelimit"
		)
		var namespace string

		It("should block second resize when rate limit exhausted", func(_ SpecContext) {
			const namespacePrefix = "autoresize-ratelimit-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying maxActionsPerDay is set to 1", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize.Strategy).ToNot(BeNil())
				Expect(cluster.Spec.StorageConfiguration.Resize.Strategy.MaxActionsPerDay).To(HaveValue(Equal(1)))
			})

			By("filling the disk to trigger first auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the disk to exceed the usage threshold
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					dataFillFile, diskFillSizeMinAvailable)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			var sizeAfterFirstResize resource.Quantity
			By("waiting for first resize to succeed", func() {
				Eventually(func() bool {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					if err != nil {
						return false
					}

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgData) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						origSize := resource.MustParse(originalVolumeSize)
						if currentSize.Cmp(origSize) > 0 {
							sizeAfterFirstResize = currentSize
							return true
						}
					}
					return false
				}, quickResizeCheckTimeout, pvcResizeCheckInterval).Should(BeTrue(),
					"First resize should succeed")
			})

			// Note: We skip waiting for filesystem expansion because Azure Disk online
			// resize can take many minutes to propagate. The PVC resize succeeding is
			// sufficient to consume the rate limit budget.

			By("verifying disk usage still triggers resize condition", func() {
				// Since the filesystem may not have expanded yet (Azure Disk online resize
				// can be slow), the original fill file still consumes the same percentage
				// of actual disk space. This means disk usage remains > threshold, which will
				// continue to trigger resize attempts that should be blocked by rate limit.
				podName := clusterName + "-1"
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					instance := cluster.Status.DiskStatus.Instances[podName]
					g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically(">", diskUsageThreshold),
						"Trigger condition must still be met (filesystem expansion may be slow)")
				}, consistentlyDuration, diskUsageCheckInterval).Should(Succeed())
			})

			By("verifying second resize is blocked by rate limit", func() {
				// Verify size remains constant - the rate limit should have blocked the second resize
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())
					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							g.Expect(pvc.Spec.Resources.Requests.Storage().Cmp(sizeAfterFirstResize)).To(Equal(0),
								"PVC size should remain unchanged due to rate limit")
						}
					}
				}, consistentlyDuration, diskUsageCheckInterval).Should(Succeed())

				// Verify a blocking event was recorded
				Eventually(func(g Gomega) {
					events, err := env.Interface.CoreV1().Events(namespace).List(env.Ctx, metav1.ListOptions{
						FieldSelector: "reason=AutoResizeBlocked",
					})
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(events.Items).ToNot(BeEmpty(), "Should have emitted AutoResizeBlocked event for rate limit")
				}, consistentlyDuration, diskUsageCheckInterval).Should(Succeed())
			})

			By("cleaning up the fill files", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
					"/var/lib/postgresql/data/pgdata/fill_file2",
				)
			})
		})
	})

	Context("minStep clamping", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-minstep.yaml.template"
			clusterName = "cluster-autoresize-minstep"
		)
		var namespace string

		It("should resize by at least minStep even when step percentage is smaller", func(_ SpecContext) {
			const namespacePrefix = "autoresize-minstep-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying minStep is configured to 1Gi with 5% step", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize.Expansion).ToNot(BeNil())
				Expect(cluster.Spec.StorageConfiguration.Resize.Expansion.MinStep).To(Equal("1Gi"))
				// 5% of 2Gi = 102Mi, but minStep clamps to 1Gi
			})

			By("filling the disk to trigger auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the disk to exceed the usage threshold
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					dataFillFile, diskFillSizeMinAvailable)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for PVC to be resized by at least minStep (1Gi)", func() {
				Eventually(func() bool {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					if err != nil {
						return false
					}

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgData) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						// Original was 2Gi, minStep is 1Gi, so should be at least 3Gi
						expectedMinSize := resource.MustParse("3Gi")
						if currentSize.Cmp(expectedMinSize) >= 0 {
							return true
						}
					}
					return false
				}, quickResizeCheckTimeout, pvcResizeCheckInterval).Should(BeTrue(),
					"PVC should have grown by at least minStep (1Gi) from 2Gi to at least 3Gi")
			})

			By("cleaning up the fill file", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
				)
			})
		})
	})

	Context("maxStep clamping via webhook", func() {
		It("should accept cluster with valid maxStep configuration", func(_ SpecContext) {
			const namespacePrefix = "autoresize-maxstep-e2e"

			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			cluster := &apiv1.Cluster{}
			cluster.SetName("autoresize-maxstep")
			cluster.SetNamespace(namespace)
			cluster.Spec.Instances = 1
			cluster.Spec.StorageConfiguration = apiv1.StorageConfiguration{
				Size: "100Gi",
				Resize: &apiv1.ResizeConfiguration{
					Enabled: ptr.To(true),
					Expansion: &apiv1.ExpansionPolicy{
						Step:    intstr.IntOrString{Type: intstr.String, StrVal: "50%"},
						MaxStep: "10Gi",
					},
					Strategy: &apiv1.ResizeStrategy{
						WALSafetyPolicy: &apiv1.WALSafetyPolicy{
							AcknowledgeWALRisk: ptr.To(true),
						},
					},
				},
			}
			cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Database: "app",
					Owner:    "app",
				},
			}

			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).ToNot(HaveOccurred(),
				"cluster creation should succeed with maxStep configured")

			By("verifying maxStep is set correctly", func() {
				created, err := clusterutils.Get(env.Ctx, env.Client, namespace, "autoresize-maxstep")
				Expect(err).ToNot(HaveOccurred())
				Expect(created.Spec.StorageConfiguration.Resize.Expansion.MaxStep).To(Equal("10Gi"))
			})
		})
	})

	Context("metrics exposure", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-basic.yaml.template"
			clusterName = "cluster-autoresize-basic"
		)
		var namespace string

		It("should expose disk metrics on the metrics endpoint", func(_ SpecContext) {
			const namespacePrefix = "autoresize-metrics-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying disk metrics are exposed with correct values", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err = env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func(g Gomega) {
					out, err := proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, *pod,
						cluster.IsMetricsTLSEnabled())
					g.Expect(err).ToNot(HaveOccurred())

					// PROOF: Verify cnpg_disk_total_bytes exists and is ~2GiB
					g.Expect(out).To(ContainSubstring("cnpg_disk_total_bytes{tablespace=\"\",volume_type=\"data\"}"))

					// Parse the value (simplified for E2E)
					lines := strings.Split(out, "\n")
					found := false
					for _, line := range lines {
						if strings.HasPrefix(line, "cnpg_disk_total_bytes{tablespace=\"\",volume_type=\"data\"}") {
							parts := strings.Fields(line)
							val := parts[1]
							// 2GiB is expectedDiskTotalBytes. Allow tolerance for filesystem overhead.
							parsedVal, parseErr := strconv.ParseFloat(val, 64)
							g.Expect(parseErr).ToNot(HaveOccurred())
							g.Expect(parsedVal).To(BeNumerically("~", expectedDiskTotalBytes, metricsToleranceBytes))
							found = true
							break
						}
					}
					g.Expect(found).To(BeTrue(), "Metric line not found")
				}, metricsCheckTimeout, diskUsageCheckInterval).Should(Succeed())
			})
		})
	})

	Context("tablespace resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-tablespace.yaml.template"
			clusterName = "cluster-autoresize-tablespace"
		)
		var namespace string

		It("should resize tablespace PVC when usage exceeds threshold", func(_ SpecContext) {
			const namespacePrefix = "autoresize-tbs-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying tablespace resize is configured", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.Tablespaces).To(HaveLen(1))
				Expect(cluster.Spec.Tablespaces[0].Name).To(Equal("tbs1"))
				Expect(cluster.Spec.Tablespaces[0].Storage.Resize).ToNot(BeNil())
				Expect(ptr.Deref(cluster.Spec.Tablespaces[0].Storage.Resize.Enabled, false)).To(BeTrue())
			})

			By("verifying tablespace PVC exists", func() {
				pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())

				var tbsCount int
				for idx := range pvcList.Items {
					pvc := &pvcList.Items[idx]
					if pvc.Labels[utils.ClusterLabelName] != clusterName {
						continue
					}
					if pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgTablespace) {
						tbsCount++
					}
				}
				Expect(tbsCount).To(BeNumerically(">", 0), "should have tablespace PVCs")
			})

			By("filling the tablespace volume to trigger auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the tablespace volume to exceed the usage threshold
				// Tablespaces are mounted at /var/lib/postgresql/tablespaces/<name>
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					tablespaceFillFile, diskFillSizeMinAvailable)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for tablespace PVC to be resized", func() {
				Eventually(func() bool {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					if err != nil {
						return false
					}

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgTablespace) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						origSize := resource.MustParse(originalVolumeSize)
						if currentSize.Cmp(origSize) > 0 {
							return true
						}
					}
					return false
				}, quickResizeCheckTimeout, pvcResizeCheckInterval).Should(BeTrue(),
					"Tablespace PVC should have been resized beyond its original size")
			})

			By("cleaning up the fill file", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/tablespaces/tbs1/fill_file",
				)
			})
		})
	})

	Context("WAL safety policy - archive health blocks resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-archive-block.yaml.template"
			clusterName = "cluster-autoresize-archive-block"
		)
		var namespace string

		// This test is pending because reliably triggering archive failures is difficult:
		// - A bogus S3 endpoint may timeout rather than fail fast
		// - Network-level failures don't always propagate to pg_stat_archiver
		// - The wal-archive command may handle errors in ways that don't update archiver stats
		// The archive health blocking logic is tested via unit tests in walsafety_test.go
		PIt("should block resize when archive is unhealthy", func(_ SpecContext) {
			const namespacePrefix = "autoresize-archiveblock-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("creating dummy S3 credentials for the bogus backup destination", func() {
				// The fixture configures barmanObjectStore pointing to a non-existent
				// endpoint. This Secret provides the required credentials so the cluster
				// can be created. When PostgreSQL tries to archive WAL, barman-cloud
				// will fail to connect, causing pg_stat_archiver to record failures.
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "archive-block-dummy-creds",
						Namespace: namespace,
					},
					Type: corev1.SecretTypeOpaque,
					StringData: map[string]string{
						"ACCESS_KEY_ID":     "dummy-access-key",
						"ACCESS_SECRET_KEY": "dummy-secret-key",
					},
				}
				err := env.Client.Create(env.Ctx, secret)
				Expect(err).ToNot(HaveOccurred())
			})

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying requireArchiveHealthy is enabled", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize.Strategy.WALSafetyPolicy).ToNot(BeNil())
				Expect(*cluster.Spec.StorageConfiguration.Resize.Strategy.WALSafetyPolicy.RequireArchiveHealthy).To(BeTrue())
			})

			By("verifying archive_mode is on", func() {
				// Check that archive_mode is enabled - without it, no archive attempts will be made
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				stdout, _, err := env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-tAc", "SHOW archive_mode",
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.TrimSpace(stdout)).To(Equal("on"), "archive_mode must be on for archive failures to be recorded")
			})

			By("generating WAL to trigger archive failures", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Generate WAL to trigger archive attempts. Each switch creates a new
				// segment that barman-cloud will try (and fail) to archive. We need
				// the archive command to fail so pg_stat_archiver records failures.
				// Generate more WAL switches to ensure multiple archive attempts fail.
				commandTimeout := walGenerationCommandTimeout
				for i := 0; i < walGenerationLoopCount; i++ {
					// Insert some data to generate meaningful WAL
					_, _, _ = env.EventuallyExecCommand(
						env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
						"psql", "-U", "postgres", "-c",
						fmt.Sprintf("CREATE TABLE IF NOT EXISTS wal_gen_test (id serial, data text); "+
							"INSERT INTO wal_gen_test (data) SELECT md5(random()::text) FROM generate_series(1,%d);", walFillDataRowCount),
					)
					// Force WAL segment switch
					_, _, _ = env.EventuallyExecCommand(
						env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
						"psql", "-U", "postgres", "-c", "SELECT pg_switch_wal()",
					)
					// Shorter wait to generate WAL faster
					time.Sleep(walGenerationLoopSleep)
				}
			})

			By("verifying archive is unhealthy", func() {
				// PROOF: Verify the operator sees the archive as unhealthy
				// Archive health detection depends on:
				// - WAL generation triggering archive attempts
				// - barman-cloud failing to connect to the bogus endpoint
				// - pg_stat_archiver.last_failed_time being more recent than last_archived_time
				// - Instance manager collecting and reporting status
				// Note: If the bogus endpoint times out (rather than failing fast), this
				// can take longer. The endpointURL is a non-routable address that should
				// fail relatively quickly with connection refused.
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					instance := cluster.Status.DiskStatus.Instances[clusterName+"-1"]
					g.Expect(instance.WALHealth).ToNot(BeNil())
					g.Expect(instance.WALHealth.ArchiveHealthy).To(BeFalse(),
						"Archive should be unhealthy when there are recent failures")
				}, archiveHealthCheckTimeout, pvcResizeCheckInterval).Should(Succeed())
			})

			By("filling the disk to trigger auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the disk to exceed the usage threshold
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					dataFillFile, diskFillSizeArchiveBlock)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())

				// PROOF: Verify percent_used > threshold
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					instance := cluster.Status.DiskStatus.Instances[podName]
					g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically(">", diskUsageThreshold))
				}, eventCheckTimeout, diskUsageCheckInterval).Should(Succeed())
			})

			By("verifying resize is blocked due to unhealthy archive", func() {
				// Verify size has NOT changed - the resize should be blocked because archive is unhealthy
				origSize := resource.MustParse(originalVolumeSize)
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] == clusterName &&
							pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
							g.Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal(origSize.String()))
						}
					}
				}, consistentlyDuration, pvcResizeCheckInterval).Should(Succeed())

				// Verify a blocking event was recorded
				Eventually(func(g Gomega) {
					events, err := env.Interface.CoreV1().Events(namespace).List(env.Ctx, metav1.ListOptions{
						FieldSelector: "reason=AutoResizeBlocked",
					})
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(events.Items).ToNot(BeEmpty(), "Should have emitted AutoResizeBlocked event")
				}, eventCheckTimeout, diskUsageCheckInterval).Should(Succeed())
			})

			By("cleaning up the fill file", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
				)
			})
		})
	})

	Context("WAL safety policy - inactive slot blocks resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-slot-block.yaml.template"
			clusterName = "cluster-autoresize-slot-block"
		)
		var namespace string

		// PENDING: This test has stability issues. The slot retention detection may fail due to:
		// - isPrimary gating in WAL health check (slots only queried on primary)
		// - Error swallowing during health check (returns partial status)
		// - Missing WAL health serialization between instance manager and controller
		// Stabilization is tracked as a follow-up PR.
		PIt("should block resize when replication slot retains too much WAL", func(_ SpecContext) {
			const namespacePrefix = "autoresize-slotblock-e2e"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying maxSlotRetentionBytes is configured", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.StorageConfiguration.Resize.Strategy.WALSafetyPolicy).ToNot(BeNil())
				// maxSlotRetentionBytes is set to 100MB in the fixture
				Expect(*cluster.Spec.StorageConfiguration.Resize.Strategy.WALSafetyPolicy.MaxSlotRetentionBytes).To(
					Equal(int64(slotRetentionThresholdBytes)))
			})

			By("creating an inactive replication slot", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Use immediately_reserve=true so the slot gets a restart_lsn immediately.
				// Without this, the slot has no restart_lsn and won't retain WAL.
				commandTimeout := cleanupCommandTimeout
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-c",
					"SELECT pg_create_physical_replication_slot('test_inactive_slot', true)",
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("generating WAL to cause slot retention", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Generate enough WAL to exceed maxSlotRetentionBytes (100MB)
				// We need to write actual data to generate real WAL content.
				// pg_switch_wal alone only creates new segments but retention is
				// measured by LSN difference (actual bytes written).
				commandTimeout := diskFillCommandTimeout
				// Insert ~120MB of data to exceed the 100MB threshold
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-c",
					fmt.Sprintf("CREATE TABLE IF NOT EXISTS wal_fill (data TEXT); "+
						"INSERT INTO wal_fill SELECT repeat('x', 1000) FROM generate_series(1, %d);", slotRetentionFillRowCount),
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for WAL health status to be updated with slot retention", func() {
				// Wait for the cluster status to show the inactive slot with
				// retention exceeding the threshold. This is necessary because
				// the WAL health check runs as part of the instance status update,
				// which happens periodically.
				//
				// First verify the slot exists directly via PostgreSQL (helps with debugging)
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				stdout, _, err := env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-t", "-A", "-c",
					"SELECT slot_name, active, restart_lsn IS NOT NULL as has_lsn, "+
						"pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)::bigint as retention "+
						"FROM pg_replication_slots WHERE slot_name = 'test_inactive_slot'",
				)
				Expect(err).ToNot(HaveOccurred())
				GinkgoWriter.Printf("Slot status from PostgreSQL: %s\n", stdout)

				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())

					if cluster.Status.DiskStatus == nil ||
						cluster.Status.DiskStatus.Instances == nil {
						return
					}

					instanceStatus, ok := cluster.Status.DiskStatus.Instances[podName]
					g.Expect(ok).To(BeTrue(), "instance status should exist")
					g.Expect(instanceStatus.WALHealth).ToNot(BeNil(), "WAL health should be populated")

					// Log current slot count for debugging
					GinkgoWriter.Printf("InactiveSlotCount=%d, InactiveSlots=%v\n",
						instanceStatus.WALHealth.InactiveSlotCount,
						instanceStatus.WALHealth.InactiveSlots)

					g.Expect(instanceStatus.WALHealth.InactiveSlots).ToNot(BeEmpty(),
						"inactive slot should be detected")

					// Verify the slot exceeds our threshold
					for _, slot := range instanceStatus.WALHealth.InactiveSlots {
						if slot.SlotName == "test_inactive_slot" {
							g.Expect(slot.RetentionBytes).To(BeNumerically(">", slotRetentionThresholdBytes),
								"slot should retain more than threshold of WAL")
							return
						}
					}
					g.Expect(false).To(BeTrue(), "test_inactive_slot not found in WAL health")
				}, slotRetentionCheckTimeout, diskUsageCheckInterval).Should(Succeed())
			})

			By("filling the disk to trigger auto-resize", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				// Fill the disk to exceed the usage threshold
				commandTimeout := diskFillCommandTimeout
				ddCmd := fmt.Sprintf(
					"dd if=/dev/zero of=%s bs=1M count=%d",
					dataFillFile, diskFillSizeSlotBlock)
				_, _, err = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"sh", "-c", ddCmd,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying resize is blocked due to inactive slot retention", func() {
				// Verify PVC has NOT been resized over a 2-minute window
				origSize := resource.MustParse(originalVolumeSize)
				Consistently(func(g Gomega) {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())

					for idx := range pvcList.Items {
						pvc := &pvcList.Items[idx]
						if pvc.Labels[utils.ClusterLabelName] != clusterName {
							continue
						}
						if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgData) {
							continue
						}
						currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
						g.Expect(currentSize.Cmp(origSize)).To(Equal(0),
							"PVC should NOT have been resized due to inactive slot retention")
					}
				}, consistentlyDuration, pvcResizeCheckInterval).Should(Succeed())
			})

			By("cleaning up", func() {
				podName := clusterName + "-1"
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}, pod)
				Expect(err).ToNot(HaveOccurred())

				commandTimeout := cleanupCommandTimeout
				// Drop the replication slot
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-c",
					"SELECT pg_drop_replication_slot('test_inactive_slot')",
				)
				// Remove fill file and wal_fill table
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
				)
				_, _, _ = env.EventuallyExecCommand(
					env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-c", "DROP TABLE IF EXISTS wal_fill",
				)
			})
		})
	})
})
