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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replication Slot", Label(tests.LabelReplication), func() {
	const (
		namespacePrefix  = "replication-slot-e2e"
		sampleFile       = fixturesDir + "/replication_slot/cluster-pg-replication-slot-disable.yaml.template"
		level            = tests.High
		userPhysicalSlot = "test_slot"
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("Can enable and disable replication slots", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("enabling replication slot on cluster", func() {
			err := replicationslot.ToggleHAReplicationSlots(
				env.Ctx, env.Client,
				namespace, clusterName, true)
			Expect(err).ToNot(HaveOccurred())

			// Replication slots should be Enabled
			Consistently(func() (bool, error) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false, err
				}
				return cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled(), nil
			}, 10, 2).Should(BeTrue())
		})

		By("checking Primary HA slots exist and are active", func() {
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
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
			Eventually(func(g Gomega) {
				replicaPods, err = clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			}, 90, 2).Should(Succeed())
			before := time.Now()
			for _, pod := range replicaPods.Items {
				expectedSlots, err := replicationslot.GetExpectedHAReplicationSlotsOnPod(
					env.Ctx, env.Client,
					namespace, clusterName, pod.GetName(),
				)
				Expect(err).ToNot(HaveOccurred())
				AssertReplicationSlotsOnPod(namespace, clusterName, pod, expectedSlots, true, false)
			}
			GinkgoWriter.Println("Replica pods slot check succeeded in", time.Since(before))
		})

		By("checking all the slots restart_lsn's are aligned", func() {
			AssertClusterReplicationSlotsAligned(namespace, clusterName)
		})

		By("creating a physical replication slots on the primary", func() {
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			query := fmt.Sprintf("SELECT pg_catalog.pg_create_physical_replication_slot('%s')", userPhysicalSlot)
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
			Eventually(func(g Gomega) {
				replicaPods, err = clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			}, 90, 2).Should(Succeed())
			before := time.Now()
			for _, pod := range replicaPods.Items {
				AssertReplicationSlotsOnPod(namespace, clusterName, pod, []string{userPhysicalSlot}, false, false)
			}
			GinkgoWriter.Println("Slot:", userPhysicalSlot, "synchronized to replica pods in", time.Since(before))
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
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false, err
				}
				return cluster.Spec.ReplicationSlots.GetEnabled(), nil
			}, 10, 2).Should(BeFalse())
		})

		By("verifying slots have been removed from the cluster's pods", func() {
			pods, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range pods.Items {
				Eventually(func(g Gomega) {
					currentSlots, err := replicationslot.GetReplicationSlotsOnPod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						pod.Namespace, pod.Name, postgres.AppDBName)
					g.Expect(err).ToNot(HaveOccurred())

					// In the primary we should retain just the user-created slot
					if specs.IsPodPrimary(pod) {
						g.Expect(currentSlots).To(HaveLen(1),
							"Slots %v should contain only %s on primary pod %s",
							currentSlots, userPhysicalSlot, pod.Name)
						g.Expect(currentSlots).To(ContainElement(userPhysicalSlot),
							"Slot %s not found on primary pod %s", userPhysicalSlot, pod.Name)
					} else {
						// In the replicas all slots should be removed
						g.Expect(currentSlots).To(BeEmpty(),
							"Slots %v still exist on replica pod %s", currentSlots, pod.Name)
					}
				}, 120, 2).Should(Succeed())
			}
		})
	})
})
