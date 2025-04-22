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

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replication Slot", Label(tests.LabelReplication), func() {
	const (
		namespacePrefix = "replication-slot-e2e"
		clusterName     = "cluster-pg-replication-slot"
		sampleFile      = fixturesDir + "/replication_slot/cluster-pg-replication-slot-disable.yaml.template"
		level           = tests.High
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
			err := replicationslot.ToggleReplicationSlots(
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
			AssertReplicationSlotsOnPod(namespace, clusterName, *primaryPod)
		})

		By("checking standbys HA slots exist", func() {
			var replicaPods *corev1.PodList
			var err error
			before := time.Now()
			Eventually(func(g Gomega) {
				replicaPods, err = clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			}, 90, 2).Should(Succeed())
			GinkgoWriter.Println("standby slot check succeeded in", time.Since(before))
			for _, pod := range replicaPods.Items {
				AssertReplicationSlotsOnPod(namespace, clusterName, pod)
			}
		})

		By("checking all the slots restart_lsn's are aligned", func() {
			AssertClusterReplicationSlotsAligned(namespace, clusterName)
		})

		By("disabling replication slot from running cluster", func() {
			err := replicationslot.ToggleReplicationSlots(
				env.Ctx, env.Client,
				namespace, clusterName, false)
			Expect(err).ToNot(HaveOccurred())

			// Replication slots should be Disabled
			Consistently(func() (bool, error) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false, err
				}
				return cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled(), nil
			}, 10, 2).Should(BeFalse())
		})

		By("verifying slots have been removed from the cluster's pods", func() {
			pods, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range pods.Items {
				Eventually(func(g Gomega) error {
					slotOnPod, err := replicationslot.GetReplicationSlotsOnPod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						namespace, pod.GetName(), postgres.AppDBName)
					if err != nil {
						return err
					}
					g.Expect(slotOnPod).To(BeEmpty())
					return nil
				}, 90, 2).ShouldNot(HaveOccurred())
			}
		})
	})
})
