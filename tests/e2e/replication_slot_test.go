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

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replication Slot", func() {
	const (
		sampleFileWithRSEnable  = fixturesDir + "/replication_slot/cluster-pg-replication-slot.yaml.template"
		sampleFileWithRSDisable = fixturesDir + "/replication_slot/cluster-pg-replication-slot-disable.yaml.template"
		level                   = tests.High
	)
	var namespace, clusterName string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if env.PostgresVersion == 10 {
			Skip("Test will be skipped for PostgreSQL 10, replication slot " +
				"high availability requires PostgreSQL 11 or above")
		}
	})
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can manage Replication slots for HA", func() {
		namespace = "replication-slot-e2e"
		clusterName = "cluster-pg-replication-slot"
		AssertCreateNamespace(namespace, env)

		// Create a cluster in a namespace we'll delete after the test
		AssertCreateCluster(namespace, clusterName, sampleFileWithRSEnable, env)

		// Gather the current primary
		oldPrimaryPod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking Primary HA Slots exist and are active", func() {
			AssertRepSlotsOnPod(namespace, clusterName, *oldPrimaryPod)
		})

		By("Checking Standbys HA Slots exist", func() {
			replicaPods, err := env.GetClusterReplicas(namespace, clusterName)
			Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			for _, pod := range replicaPods.Items {
				AssertRepSlotsOnPod(namespace, clusterName, pod)
			}
		})

		By("Checking all the slots restart_lsn's are aligned", func() {
			AssertClusterRepSlotsAligned(namespace, clusterName)
		})

		By("Creating test data to advance streaming replication", func() {
			tableName := "data"
			AssertCreateTestData(namespace, clusterName, tableName)
			// Generate some WAL load
			query := fmt.Sprintf("INSERT INTO %v (SELECT generate_series(1,10000))",
				tableName)
			_, _, err = testsUtils.RunQueryFromPod(oldPrimaryPod, testsUtils.PGLocalSocketDir,
				"app", "postgres", "''", query, env)
			Expect(err).ToNot(HaveOccurred())
			_ = switchWalAndGetLatestArchive(namespace, oldPrimaryPod.Name)
		})

		By("Deleting the primary pod", func() {
			zero := int64(0)
			timeout := 120
			forceDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			err = env.DeletePod(namespace, oldPrimaryPod.Name, forceDelete)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, clusterName, timeout, env)
		})

		By("Checking that all the slots exist and are aligned", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				AssertRepSlotsOnPod(namespace, clusterName, pod)
			}
			AssertClusterRepSlotsAligned(namespace, clusterName)
		})
	})

	It("replication slots can manage on disable/enable", func() {
		namespace = "replication-slot-disable-e2e"
		clusterName = "cluster-pg-replication-slot-disable"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFileWithRSDisable, env)

		By("enabling replication slot on cluster", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			clusterEnableRepSlot := cluster.DeepCopy()
			clusterEnableRepSlot.Spec.ReplicationSlots.HighAvailability.Enabled = true
			err = env.Client.Patch(env.Ctx, clusterEnableRepSlot, ctrlclient.MergeFrom(cluster))
			Expect(err).ToNot(HaveOccurred())

			// replication slot should be true
			Consistently(func() (bool, error) {
				cluster, err := env.GetCluster(namespace, clusterName)
				if err != nil {
					return false, err
				}
				return cluster.Spec.ReplicationSlots.HighAvailability.Enabled, nil
			}, 10, 2).Should(BeTrue())
		})

		if env.PostgresVersion == 11 {
			// We need to take into account the fact that on PostgreSQL 11
			// it is required to rolling restart the cluster to
			// enable or disable the feature once the cluster is created.
			AssertClusterRollingRestart(namespace, clusterName)
		}

		By("checking Primary HA slots exist and are active", func() {
			primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertRepSlotsOnPod(namespace, clusterName, *primaryPod)
		})

		By("checking standbys HA slots exist", func() {
			replicaPods, err := env.GetClusterReplicas(namespace, clusterName)
			Eventually(func(g Gomega) {
				g.Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			}, 90, 2).Should(Succeed())
			for _, pod := range replicaPods.Items {
				AssertRepSlotsOnPod(namespace, clusterName, pod)
			}
		})

		By("checking all the slots restart_lsn's are aligned", func() {
			AssertClusterRepSlotsAligned(namespace, clusterName)
		})

		By("disabling replication slot from running cluster", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			clusterDisableReplSlot := cluster.DeepCopy()
			clusterDisableReplSlot.Spec.ReplicationSlots.HighAvailability.Enabled = false
			err = env.Client.Patch(env.Ctx, clusterDisableReplSlot, ctrlclient.MergeFrom(cluster))
			Expect(err).ToNot(HaveOccurred())

			// check that, replication slot will disable
			cluster, err = env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Spec.ReplicationSlots.HighAvailability.Enabled).Should(BeFalse())
		})

		if env.PostgresVersion == 11 {
			// We need to take into account the fact that on PostgreSQL 11
			// it is required to rolling restart the cluster to
			// enable or disable the feature once the cluster is created.
			AssertClusterRollingRestart(namespace, clusterName)
		}

		By("verifying slots has removed from cluster pods", func() {
			pods, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range pods.Items {
				Eventually(func() (int, error) {
					slotOnPod, err := testsUtils.GetRepSlotsOnPod(namespace, pod.GetName(), env)
					if err != nil {
						return -1, err
					}
					return len(slotOnPod), nil
				}, 90, 2).Should(BeEquivalentTo(0))
			}
		})
	})
})
