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
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Dynamic storage helper constants
const (
	// Timeouts for helper functions
	helperDiskFillTimeout       = 120 * time.Second
	helperCleanupTimeout        = 30 * time.Second
	helperDiskUsageCheckTimeout = 120 * time.Second
	helperDiskUsageInterval     = 5 * time.Second
	helperPVCResizeInterval     = 10 * time.Second
	helperEventCheckTimeout     = 2 * time.Minute
	helperEventCheckInterval    = 5 * time.Second
	helperPVCSizeCheckTimeout   = 1 * time.Minute
	helperNewReplicaTimeout     = 5 * time.Minute
	helperWALFillTimeout        = 120 * time.Second
	helperDataIntegrityTimeout  = 60 * time.Second
)

// fillDiskToTriggerResize fills disk on a specific pod to trigger auto-resize
func fillDiskToTriggerResize(namespace, podName string, fillMB int) {
	pod := &corev1.Pod{}
	err := env.Client.Get(env.Ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}, pod)
	Expect(err).ToNot(HaveOccurred())

	commandTimeout := helperDiskFillTimeout
	_, _, err = env.EventuallyExecCommand(
		env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
		"sh", "-c",
		fmt.Sprintf("dd if=/dev/zero of=/var/lib/postgresql/data/pgdata/fill_file bs=1M count=%d || true", fillMB),
	)
	Expect(err).ToNot(HaveOccurred())
}

// assertDiskUsageExceedsThreshold verifies disk usage exceeds threshold
func assertDiskUsageExceedsThreshold(namespace, clusterName, podName string, threshold int) {
	Eventually(func(g Gomega) {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(cluster.Status.DiskStatus).ToNot(BeNil())
		instance, ok := cluster.Status.DiskStatus.Instances[podName]
		g.Expect(ok).To(BeTrue(), "DiskStatus should contain instance %s", podName)
		g.Expect(instance.DataVolume).ToNot(BeNil())
		g.Expect(instance.DataVolume.PercentUsed).To(BeNumerically(">", threshold),
			"Disk usage on %s must exceed threshold to trigger resize", podName)
	}, helperDiskUsageCheckTimeout, helperDiskUsageInterval).Should(Succeed())
}

// assertPVCResized verifies PVC has been resized beyond originalSize
func assertPVCResized(namespace, clusterName string, originalSize resource.Quantity, timeout time.Duration) {
	Eventually(func(g Gomega) {
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
		g.Expect(err).ToNot(HaveOccurred())

		resizedCount := 0
		for idx := range pvcList.Items {
			pvc := &pvcList.Items[idx]
			if pvc.Labels[utils.ClusterLabelName] == clusterName &&
				pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
				currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
				if currentSize.Cmp(originalSize) > 0 {
					resizedCount++
				}
			}
		}
		g.Expect(resizedCount).To(BeNumerically(">", 0), "At least one PVC should be resized")
	}, timeout, helperPVCResizeInterval).Should(Succeed())
}

// assertDataIntegrity verifies data integrity using a sentinel table
func assertDataIntegrity(namespace, clusterName, tableName string, expectedRows int) {
	primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())

	commandTimeout := helperCleanupTimeout
	Eventually(func(g Gomega) {
		stdout, _, err := env.EventuallyExecCommand(
			env.Ctx, *primary, specs.PostgresContainerName, &commandTimeout,
			"psql", "-U", "postgres", "-tAc",
			fmt.Sprintf("SELECT count(*) FROM %s", tableName),
		)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(stdout).To(ContainSubstring(fmt.Sprintf("%d", expectedRows)))
	}, helperDataIntegrityTimeout, helperDiskUsageInterval).Should(Succeed())
}

// createSentinelData creates sentinel data for integrity checks
func createSentinelData(namespace, clusterName, tableName string, rows int) {
	primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())

	commandTimeout := helperCleanupTimeout
	_, _, err = env.EventuallyExecCommand(
		env.Ctx, *primary, specs.PostgresContainerName, &commandTimeout,
		"psql", "-U", "postgres", "-c",
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id serial PRIMARY KEY, data text, created_at timestamp default now()); "+
			"INSERT INTO %s (data) SELECT md5(random()::text) FROM generate_series(1, %d);",
			tableName, tableName, rows),
	)
	Expect(err).ToNot(HaveOccurred())
}

// cleanupFillFiles cleans up fill files from a pod
func cleanupFillFiles(namespace, podName string) {
	pod := &corev1.Pod{}
	err := env.Client.Get(env.Ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}, pod)
	if err != nil {
		GinkgoWriter.Printf("warning: failed to get pod %s for cleanup: %v\n", podName, err)
		return
	}

	commandTimeout := helperCleanupTimeout
	_, _, err = env.EventuallyExecCommand(
		env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
		"rm", "-f", "/var/lib/postgresql/data/pgdata/fill_file",
	)
	if err != nil {
		GinkgoWriter.Printf("warning: failed to cleanup fill files on %s: %v\n", podName, err)
	}
}

// assertNewReplicaPVCSize verifies new replica PVC has expected size
func assertNewReplicaPVCSize(namespace, pvcName string, expectedMinSize resource.Quantity) {
	Eventually(func(g Gomega) {
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
		g.Expect(err).ToNot(HaveOccurred())

		var newPVCSize resource.Quantity
		found := false

		for idx := range pvcList.Items {
			pvc := &pvcList.Items[idx]
			if pvc.Name == pvcName &&
				pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
				newPVCSize = pvc.Spec.Resources.Requests[corev1.ResourceStorage]
				found = true
				break
			}
		}

		g.Expect(found).To(BeTrue(), "New replica PVC %s should exist", pvcName)
		g.Expect(newPVCSize.Cmp(expectedMinSize)).To(BeNumerically(">=", 0),
			"New replica PVC should be at effective operational size (%v), not stale bootstrap size",
			expectedMinSize.String())
	}, helperNewReplicaTimeout, helperPVCResizeInterval).Should(Succeed())
}

// waitForResizeTriggered waits for a resize operation to be triggered by watching events
func waitForResizeTriggered(namespace, clusterName string) {
	Eventually(func(g Gomega) {
		events, err := env.Interface.CoreV1().Events(namespace).List(env.Ctx, metav1.ListOptions{})
		g.Expect(err).ToNot(HaveOccurred())

		triggered := false
		for _, event := range events.Items {
			if event.InvolvedObject.Kind == "Cluster" &&
				event.InvolvedObject.Name == clusterName {
				if event.Reason == "AutoResizeTriggered" ||
					event.Reason == "AutoResizeSuccess" ||
					event.Reason == "AutoResizeStarted" ||
					event.Reason == "ResizeInProgress" {
					triggered = true
					GinkgoWriter.Printf("[EVENT] Resize triggered: %s - %s\n", event.Reason, event.Message)
					break
				}
			}
		}

		// Also check cluster status for resize activity
		if !triggered {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			if len(cluster.Status.AutoResizeEvents) > 0 {
				triggered = true
				GinkgoWriter.Printf("[STATUS] Resize triggered via AutoResizeEvents\n")
			}
		}

		g.Expect(triggered).To(BeTrue(), "Resize operation should be triggered")
	}, helperEventCheckTimeout, helperEventCheckInterval).Should(Succeed())
}

// assertClusterStandbysAreStreaming validates that all standbys are streaming from the primary
func assertClusterStandbysAreStreaming(namespace, clusterName string, timeoutSeconds int) {
	By("verifying all standbys are streaming", func() {
		Eventually(func(g Gomega) {
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			replicaCount, err := postgres.CountReplicas(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				primary, timeoutSeconds,
			)
			g.Expect(err).ToNot(HaveOccurred())

			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			expectedReplicas := cluster.Spec.Instances - 1
			g.Expect(replicaCount).To(BeEquivalentTo(expectedReplicas),
				"Expected %d streaming replicas, got %d", expectedReplicas, replicaCount)
		}, time.Duration(timeoutSeconds)*time.Second, helperDiskUsageInterval).Should(Succeed())
	})
}

// fillWALToTriggerResize fills WAL volume to trigger auto-resize
func fillWALToTriggerResize(namespace, podName string, fillMB int) {
	pod := &corev1.Pod{}
	err := env.Client.Get(env.Ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}, pod)
	Expect(err).ToNot(HaveOccurred())

	commandTimeout := helperWALFillTimeout
	_, _, err = env.EventuallyExecCommand(
		env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
		"sh", "-c",
		fmt.Sprintf("dd if=/dev/zero of=/var/lib/postgresql/wal/pg_wal/fill_file bs=1M count=%d || true", fillMB),
	)
	Expect(err).ToNot(HaveOccurred())
}

// cleanupWALFillFiles cleans up WAL fill files from a pod
func cleanupWALFillFiles(namespace, podName string) {
	pod := &corev1.Pod{}
	err := env.Client.Get(env.Ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}, pod)
	if err != nil {
		GinkgoWriter.Printf("warning: failed to get pod %s for WAL cleanup: %v\n", podName, err)
		return
	}

	commandTimeout := helperCleanupTimeout
	_, _, err = env.EventuallyExecCommand(
		env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
		"rm", "-f", "/var/lib/postgresql/wal/pg_wal/fill_file",
	)
	if err != nil {
		GinkgoWriter.Printf("warning: failed to cleanup WAL fill files on %s: %v\n", podName, err)
	}
}

// assertWALPVCResized verifies WAL PVC has been resized
func assertWALPVCResized(namespace, clusterName string, originalSize resource.Quantity, timeout time.Duration) {
	Eventually(func(g Gomega) {
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
		g.Expect(err).ToNot(HaveOccurred())

		resizedCount := 0
		for idx := range pvcList.Items {
			pvc := &pvcList.Items[idx]
			if pvc.Labels[utils.ClusterLabelName] == clusterName &&
				pvc.Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgWal) {
				currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
				if currentSize.Cmp(originalSize) > 0 {
					resizedCount++
				}
			}
		}
		g.Expect(resizedCount).To(BeNumerically(">", 0), "At least one WAL PVC should be resized")
	}, timeout, helperPVCResizeInterval).Should(Succeed())
}
