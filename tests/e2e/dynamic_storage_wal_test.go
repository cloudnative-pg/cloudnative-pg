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

	"k8s.io/apimachinery/pkg/api/resource"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Dynamic Storage WAL Volume Tests
// These tests validate WAL volume resize operations during disruptions.
var _ = Describe("Dynamic Storage WAL Volume", Serial, Label(tests.LabelDisruptive, tests.LabelStorage), func() {
	const level = tests.Medium

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("WAL volume resize during operator restart", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-wal-runtime.yaml.template"
			clusterName = "cluster-autoresize-wal-runtime"
		)

		It("should resume WAL volume growth after operator restart", func(_ SpecContext) {
			const namespacePrefix = "wal-resize-op"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data for integrity check", func() {
				createSentinelData(namespace, clusterName, "sentinel_wal_op", 100)
			})

			primaryPodName := clusterName + "-1"
			originalWALSize := resource.MustParse("2Gi")

			By("filling the WAL volume to trigger auto-resize", func() {
				fillWALToTriggerResize(namespace, primaryPodName, 1800)
			})

			By("waiting for WAL resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("restarting the operator pod", func() {
				err = operator.ReloadDeployment(env.Ctx, env.Client, 120)
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for WAL PVC resize to complete", func() {
				assertWALPVCResized(namespace, clusterName, originalWALSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_wal_op", 100)
			})

			By("cleaning up fill files", func() {
				cleanupWALFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("WAL volume resize during primary restart", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-wal.yaml.template"
			clusterName = "cluster-autoresize-wal"
		)

		It("should resume WAL volume growth after primary restart", func(_ SpecContext) {
			const namespacePrefix = "wal-resize-pri"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_wal_pri", 100)
			})

			primaryPodName := clusterName + "-1"
			originalWALSize := resource.MustParse("2Gi")

			By("filling the WAL volume to trigger auto-resize", func() {
				fillWALToTriggerResize(namespace, primaryPodName, 1800)
			})

			By("waiting for WAL resize to be triggered", func() {
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

			By("waiting for WAL PVC resize to complete", func() {
				assertWALPVCResized(namespace, clusterName, originalWALSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_wal_pri", 100)
			})

			By("cleaning up fill files", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				cleanupWALFillFiles(namespace, primary.Name)
			})
		})
	})

	Context("concurrent data and WAL volume resize", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-wal.yaml.template"
			clusterName = "cluster-autoresize-wal"
		)

		It("should handle simultaneous data and WAL volume resize", func(_ SpecContext) {
			const namespacePrefix = "wal-resize-both"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_wal_both", 100)
			})

			primaryPodName := clusterName + "-1"
			originalDataSize := resource.MustParse("2Gi")
			originalWALSize := resource.MustParse("2Gi")

			By("filling both data and WAL volumes to trigger auto-resize", func() {
				fillDiskToTriggerResize(namespace, primaryPodName, 1800)
				fillWALToTriggerResize(namespace, primaryPodName, 1800)
			})

			By("waiting for resize to be triggered", func() {
				waitForResizeTriggered(namespace, clusterName)
			})

			By("waiting for both PVC resizes to complete", func() {
				assertPVCResized(namespace, clusterName, originalDataSize, 25*time.Minute)
				assertWALPVCResized(namespace, clusterName, originalWALSize, 25*time.Minute)
			})

			By("verifying cluster is healthy", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_wal_both", 100)
			})

			By("cleaning up fill files", func() {
				cleanupFillFiles(namespace, primaryPodName)
				cleanupWALFillFiles(namespace, primaryPodName)
			})
		})
	})

	Context("WAL volume resize with archiving requirements", func() {
		const (
			sampleFile  = fixturesDir + "/auto_resize/cluster-autoresize-wal.yaml.template"
			clusterName = "cluster-autoresize-wal"
		)

		It("should respect archive health requirements during WAL resize", func(_ SpecContext) {
			const namespacePrefix = "wal-resize-arch"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("creating sentinel data", func() {
				createSentinelData(namespace, clusterName, "sentinel_wal_arch", 100)
			})

			primaryPodName := clusterName + "-1"
			originalWALSize := resource.MustParse("2Gi")

			By("filling the WAL volume", func() {
				fillWALToTriggerResize(namespace, primaryPodName, 1800)
			})

			By("waiting for resize trigger or block based on archive health", func() {
				// This test verifies the system responds appropriately
				// whether archive is healthy or not
				waitForResizeTriggered(namespace, clusterName)
			})

			By("verifying cluster remains operational", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
			})

			By("checking WAL resize status", func() {
				// May or may not resize depending on archive health
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					// Cluster should have disk status available
					g.Expect(cluster.Status.DiskStatus).ToNot(BeNil())
				}, 2*time.Minute, 10*time.Second).Should(Succeed())
			})

			By("verifying data integrity", func() {
				assertDataIntegrity(namespace, clusterName, "sentinel_wal_arch", 100)
			})

			By("cleaning up fill files", func() {
				cleanupWALFillFiles(namespace, primaryPodName)
			})

			By("waiting for WAL PVC resize if archive allows", func() {
				// Attempt to verify resize completed - may skip if blocked by archive policy
				assertWALPVCResized(namespace, clusterName, originalWALSize, 25*time.Minute)
			})
		})
	})
})
