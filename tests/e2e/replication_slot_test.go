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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replication Slot", Label(tests.LabelReplication), func() {
	const (
		namespacePrefix  = "replication-slot-e2e"
		clusterName      = "cluster-pg-replication-slot"
		sampleFile       = fixturesDir + "/replication_slot/cluster-pg-replication-slot-disable.yaml.template"
		level            = tests.High
		userPhysicalSlot = "test_slot"
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("Can enable and disable replication slots", func() {
		var err error
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("enabling replication slot on cluster", func() {
			err := replicationslot.ToggleHAReplicationSlots(
				env.Ctx, env.Client,
				namespace, clusterName, true)
			Expect(err).ToNot(HaveOccurred())

			// Replication slots should be Enabled
			Consistently(func() (bool, error) {
				cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false, err
				}
				return cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled(), nil
			}, 10, 2).Should(BeTrue())
		})

		if env.PostgresVersion == 11 {
			// We need to take into account the fact that on PostgreSQL 11
			// it is required to rolling restart the cluster to
			// enable or disable the feature once the cluster is created.
			AssertClusterRollingRestart(namespace, clusterName)
		}

		By("checking Primary HA slots exist and are active", func() {
			primaryPod, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			expectedSlots, err := replicationslot.GetExpectedHAReplicationSlotsOnPod(
				env.Ctx, env.Client,
				namespace,
				clusterName,
				primaryPod.GetName(),
			)
			Expect(err).ToNot(HaveOccurred())
			AssertReplicationSlotsOnPod(namespace, clusterName, *primaryPod, expectedSlots, true, false)
		})

		By("checking standbys HA slots exist", func() {
			var replicaPods *corev1.PodList
			var err error
			before := time.Now()
			Eventually(func(g Gomega) {
				replicaPods, err = clusterutils.GetClusterReplicas(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			}, 90, 2).Should(Succeed())
			GinkgoWriter.Println("standby slot check succeeded in", time.Since(before))
			for _, pod := range replicaPods.Items {
				expectedSlots, err := replicationslot.GetExpectedHAReplicationSlotsOnPod(
					env.Ctx, env.Client,
					namespace, clusterName, pod.GetName(),
				)
				Expect(err).ToNot(HaveOccurred())
				AssertReplicationSlotsOnPod(namespace, clusterName, pod, expectedSlots, true, false)
			}
		})

		By("checking all the slots restart_lsn's are aligned", func() {
			AssertClusterReplicationSlotsAligned(namespace, clusterName)
		})

		By("creating a physical replication slots on the primary", func() {
			primaryPod, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			query := fmt.Sprintf("SELECT pg_create_physical_replication_slot('%s');", userPhysicalSlot)
			_, _, err = exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				postgres.PostgresDBName,
				query)
			Expect(err).ToNot(HaveOccurred())
		})

		By("ensuring that the new physical replication slot is found on the replicas", func() {
			var replicaPods *corev1.PodList
			var err error
			before := time.Now()
			Eventually(func(g Gomega) {
				replicaPods, err = clusterutils.GetClusterReplicas(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			}, 90, 2).Should(Succeed())
			GinkgoWriter.Println("standby slot check succeeded in", time.Since(before))
			for _, pod := range replicaPods.Items {
				GinkgoWriter.Println("checking replica pod:", pod.Name)
				AssertReplicationSlotsOnPod(namespace, clusterName, pod, []string{userPhysicalSlot}, false, false)
			}
		})

		By("disabling replication slot from running cluster", func() {
			err := replicationslot.ToggleHAReplicationSlots(
				env.Ctx, env.Client,
				namespace, clusterName, false)
			Expect(err).ToNot(HaveOccurred())
			err = replicationslot.ToggleSynchronizeReplicationSlots(
				env.Ctx, env.Client,
				namespace, clusterName, false)
			Expect(err).ToNot(HaveOccurred())

			// Replication slots should be Disabled
			Consistently(func() (bool, error) {
				cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false, err
				}
				return cluster.Spec.ReplicationSlots.GetEnabled(), nil
			}, 10, 2).Should(BeFalse())
		})

		if env.PostgresVersion == 11 {
			// We need to take into account the fact that on PostgreSQL 11
			// it is required to rolling restart the cluster to
			// enable or disable the feature once the cluster is created.
			AssertClusterRollingRestart(namespace, clusterName)
		}

		By("verifying slots have been removed from the cluster's pods", func() {
			pods, err := clusterutils.GetClusterPodList(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range pods.Items {
				Eventually(func(g Gomega) error {
					slotOnPod, err := replicationslot.GetReplicationSlotsOnPod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						namespace, pod.GetName(), postgres.AppDBName)
					if err != nil {
						return err
					}

					// on the primary we should retain the user created slot
					if specs.IsPodPrimary(pod) {
						g.Expect(slotOnPod).To(HaveLen(1))
						g.Expect(slotOnPod).To(ContainElement(userPhysicalSlot))
						return nil
					}
					// on replicas instead we should clean up everything
					g.Expect(slotOnPod).To(BeEmpty())
					return nil
				}, 90, 2).ShouldNot(HaveOccurred())
			}
		})
	})
})
