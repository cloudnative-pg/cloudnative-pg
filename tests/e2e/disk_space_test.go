/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Volume space unavailable", Label(tests.LabelStorage), func() {
	const (
		level           = tests.Low
		namespacePrefix = "diskspace-e2e"
	)

	diskSpaceDetectionTest := func(namespace, clusterName string) {
		const walDir = "/var/lib/postgresql/data/pgdata/pg_wal"
		var cluster *apiv1.Cluster
		var primaryPod *corev1.Pod
		By("finding cluster resources", func() {
			var err error
			cluster, err = env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster).ToNot(BeNil())

			primaryPod, err = env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryPod).ToNot(BeNil())
		})
		By("filling the WAL volume", func() {
			timeout := time.Minute * 5

			_, _, err := env.ExecCommandInInstancePod(
				testsUtils.PodLocator{
					Namespace: namespace,
					PodName:   primaryPod.Name,
				},
				&timeout,
				"dd", "if=/dev/zero", "of="+walDir+"/fill", "bs=1M",
			)
			Expect(err).To(HaveOccurred())
			// FIXME: check if the error is due to the disk being full
		})
		By("writing something when no space is available", func() {
			// Create the table used by the scenario
			query := "CREATE TABLE diskspace AS SELECT generate_series(1, 1000000);"
			_, _, err := env.ExecCommandWithPsqlClient(
				namespace,
				clusterName,
				primaryPod,
				apiv1.ApplicationUserSecretSuffix,
				testsUtils.AppDBName,
				query,
			)
			Expect(err).To(HaveOccurred())
			query = "CHECKPOINT; SELECT pg_switch_wal(); CHECKPOINT"
			_, _, err = env.ExecQueryInInstancePod(
				testsUtils.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				testsUtils.DatabaseName("postgres"),
				query)
			Expect(err).To(HaveOccurred())
		})
		By("waiting for the primary to become not ready", func() {
			Eventually(func(g Gomega) bool {
				primaryPod, err := env.GetPod(namespace, primaryPod.Name)
				g.Expect(err).ToNot(HaveOccurred())
				return testsUtils.PodHasCondition(primaryPod, corev1.PodReady, corev1.ConditionFalse)
			}).WithTimeout(time.Minute).Should(BeTrue())
		})
		By("checking if the operator detects the issue", func() {
			Eventually(func(g Gomega) string {
				cluster, err := env.GetCluster(namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				return cluster.Status.Phase
			}).WithTimeout(time.Minute).Should(Equal("Not enough disk space"))
		})
	}

	recoveryTest := func(namespace, clusterName string) {
		var cluster *apiv1.Cluster
		var primaryPod *corev1.Pod
		primaryWALPVC := &corev1.PersistentVolumeClaim{}
		By("finding cluster resources", func() {
			var err error
			cluster, err = env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster).ToNot(BeNil())

			primaryPod, err = env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryPod).ToNot(BeNil())

			primaryWALPVCName := primaryPod.Name
			if cluster.Spec.WalStorage != nil {
				primaryWALPVCName = fmt.Sprintf("%v-wal", primaryWALPVCName)
			}
			err = env.Client.Get(env.Ctx,
				types.NamespacedName{Namespace: primaryPod.Namespace, Name: primaryWALPVCName}, primaryWALPVC)
			Expect(err).ToNot(HaveOccurred())
		})
		By("resizing the WAL volume", func() {
			originPVC := primaryWALPVC.DeepCopy()
			newSize := *resource.NewScaledQuantity(2, resource.Giga)
			primaryWALPVC.Spec.Resources.Requests[corev1.ResourceStorage] = newSize
			Expect(env.Client.Patch(env.Ctx, primaryWALPVC, ctrlclient.MergeFrom(originPVC))).To(Succeed())
			Eventually(func(g Gomega) int64 {
				err := env.Client.Get(env.Ctx,
					types.NamespacedName{Namespace: primaryPod.Namespace, Name: primaryWALPVC.Name},
					primaryWALPVC)
				g.Expect(err).ToNot(HaveOccurred())
				size := ptr.To(primaryWALPVC.Status.Capacity[corev1.ResourceStorage]).Value()
				return size
			}).WithTimeout(time.Minute * 5).Should(BeNumerically(">=",
				newSize.Value()))
		})
		By("waiting for the primary to become ready", func() {
			// The primary Pod will be in crash loop backoff. We need
			// to wait for the Pod to restart. The maximum backoff time
			// is set in the kubelet to 5 minutes, and this parameter
			// is not configurable without recompiling the kubelet
			// itself. See:
			//
			// https://github.com/kubernetes/kubernetes/blob/
			//   1d5589e4910ed859a69b3e57c25cbbd3439cd65f/pkg/kubelet/kubelet.go#L145
			//
			// This is why we wait for 10 minutes here.
			// We can't delete the Pod, as this will trigger
			// a failover.
			Eventually(func(g Gomega) bool {
				primaryPod, err := env.GetPod(namespace, primaryPod.Name)
				g.Expect(err).ToNot(HaveOccurred())
				return testsUtils.PodHasCondition(primaryPod, corev1.PodReady, corev1.ConditionTrue)
			}).WithTimeout(10 * time.Minute).Should(BeTrue())
		})
		By("writing some WAL", func() {
			query := "CHECKPOINT; SELECT pg_switch_wal(); CHECKPOINT"
			_, _, err := env.ExecQueryInInstancePod(
				testsUtils.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				testsUtils.DatabaseName("postgres"),
				query)
			Expect(err).NotTo(HaveOccurred())
		})
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if IsLocal() {
			// Local environments use the node disk space, running out of that space could cause multiple failures
			Skip("This test is not executed on local environments")
		}
	})

	DescribeTable("WAL volume space unavailable",
		func(sampleFile string) {
			var namespace string
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})

			clusterName, err := env.GetResourceNameFromYAML(sampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("leaving a full disk pod fenced", func() {
				diskSpaceDetectionTest(namespace, clusterName)
			})
			By("being able to recover with manual intervention", func() {
				recoveryTest(namespace, clusterName)
			})
		},
		Entry("Data and WAL same volume", fixturesDir+"/disk_space/cluster-disk-space-single-volume.yaml.template"),
		Entry("Data and WAL different volume", fixturesDir+"/disk_space/cluster-disk-space-wal-volume.yaml.template"),
	)
})
