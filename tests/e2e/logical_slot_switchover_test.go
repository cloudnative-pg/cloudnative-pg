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
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests for logical slot cleanup after switchover when synchronizeLogicalDecoding is enabled.
// This tests the cleanup of orphaned failover-enabled logical slots that remain on a demoted
// primary after switchover. PostgreSQL's sync_replication_slots syncs these slots to replicas
// with synced=true, but when a primary is demoted, its locally-created slots remain with
// synced=false and must be cleaned up for the sync worker to recreate them properly.
var _ = Describe("Logical Slot Switchover", Label(tests.LabelPublicationSubscription, tests.LabelSelfHealing), func() {
	const (
		sourceClusterManifest  = fixturesDir + "/logical_slot_switchover/source-cluster.yaml.template"
		sourceDatabaseManifest = fixturesDir + "/logical_slot_switchover/source-database.yaml"
		level                  = tests.High
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("with synchronizeLogicalDecoding enabled on PG17+", Ordered, func() {
		const (
			namespacePrefix = "logical-slot-switchover"
			dbname          = "testdb"
			slotName        = "test_failover_slot"
		)
		var (
			sourceClusterName, namespace string
			err                          error
		)

		BeforeAll(func() {
			// This test requires PostgreSQL 17+ for sync_replication_slots and logical slot failover features
			currentVersion, versionErr := version.FromTag(env.PostgresImageTag)
			Expect(versionErr).ToNot(HaveOccurred())
			if currentVersion.Major() < 17 {
				Skip("This test requires PostgreSQL 17+ for sync_replication_slots and logical slot failover features")
			}

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sourceClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sourceClusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up source cluster with synchronizeLogicalDecoding", func() {
				AssertCreateCluster(namespace, sourceClusterName, sourceClusterManifest, env)
			})

			By("creating database", func() {
				CreateResourceFromFile(namespace, sourceDatabaseManifest)

				// Wait for database to be ready
				Eventually(func(g Gomega) {
					db := &apiv1.Database{}
					err := env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: "source-db"}, db)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(db.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("creating a failover-enabled logical replication slot on primary", func() {
				// Create a logical slot with failover=true directly via SQL
				// This simulates what happens when a subscription is created with failover=true
				// or when using pg_failover_slots extension
				query := "SELECT pg_create_logical_replication_slot('" + slotName + "', 'pgoutput', false, false, true)"
				_, err = postgres.RunExecOverForward(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, sourceClusterName, dbname,
					apiv1.SuperUserSecretSuffix, query,
				)
				Expect(err).ToNot(HaveOccurred())
				GinkgoWriter.Printf("Created logical slot '%s' with failover=true\n", slotName)
			})
		})

		// FlakeAttempts(1) because this test performs a switchover which changes cluster state
		// and cannot be safely retried without recreating the cluster
		It("cleans up orphaned failover logical slots after switchover", FlakeAttempts(1), func() {
			var oldPrimary string

			By("recording initial primary", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, sourceClusterName)
				Expect(err).ToNot(HaveOccurred())
				oldPrimary = cluster.Status.CurrentPrimary
				GinkgoWriter.Printf("Initial primary: %s\n", oldPrimary)
			})

			By("verifying failover slot exists on primary with correct flags", func() {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, sourceClusterName)
				Expect(err).ToNot(HaveOccurred())

				// Check that the slot has failover=true and synced=false (as expected on primary)
				query := "SELECT slot_name, slot_type, synced, failover " +
					"FROM pg_replication_slots WHERE slot_name = '" + slotName + "'"
				Eventually(func(g Gomega) {
					out, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{Namespace: primaryPod.Namespace, PodName: primaryPod.Name},
						dbname, query,
					)
					g.Expect(err).ToNot(HaveOccurred())
					GinkgoWriter.Printf("Slot on primary:\n%s\n", out)
					// On primary: synced should be false (locally created), failover should be true
					g.Expect(out).To(ContainSubstring(slotName))
					g.Expect(out).To(ContainSubstring("f")) // synced=false
					g.Expect(out).To(ContainSubstring("t")) // failover=true
				}, 60).WithPolling(5 * time.Second).Should(Succeed())
			})

			By("waiting for slot to be synced to replicas", func() {
				// PostgreSQL's sync_replication_slots should sync the failover slot to replicas
				// On replicas, synced should be true
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, sourceClusterName)
				Expect(err).ToNot(HaveOccurred())

				// Check one of the replicas
				var replicaPod string
				for _, instance := range cluster.Status.InstanceNames {
					if instance != oldPrimary {
						replicaPod = instance
						break
					}
				}
				Expect(replicaPod).ToNot(BeEmpty(), "Expected to find a replica pod")
				GinkgoWriter.Printf("Checking replica pod: %s\n", replicaPod)

				query := "SELECT slot_name, synced, failover FROM pg_replication_slots WHERE slot_name = '" + slotName + "'"
				Eventually(func(g Gomega) {
					out, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{Namespace: namespace, PodName: replicaPod},
						postgres.PostgresDBName, query,
					)
					g.Expect(err).ToNot(HaveOccurred())
					GinkgoWriter.Printf("Slot on replica %s:\n%s\n", replicaPod, out)
					// On replica: slot should exist with synced=true (synced from primary)
					g.Expect(out).To(ContainSubstring(slotName))
				}, 120).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("triggering switchover", func() {
				GinkgoWriter.Printf("Triggering switchover from %s\n", oldPrimary)
				AssertSwitchover(namespace, sourceClusterName, env)
			})

			By("verifying new primary", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, sourceClusterName)
				Expect(err).ToNot(HaveOccurred())
				GinkgoWriter.Printf("New primary: %s (old was: %s)\n", cluster.Status.CurrentPrimary, oldPrimary)
				Expect(cluster.Status.CurrentPrimary).ToNot(Equal(oldPrimary))
			})

			By("verifying orphaned failover slots are cleaned up on demoted primary", func() {
				GinkgoWriter.Printf("Checking for orphaned slots on demoted primary: %s\n", oldPrimary)
				// The old primary is now a replica
				// After CNPG's cleanup runs, slots with synced=false AND failover=true should be removed
				query := "SELECT slot_name, synced, failover FROM pg_replication_slots " +
					"WHERE slot_name = '" + slotName + "' AND synced = false AND failover = true"

				Eventually(func(g Gomega) {
					out, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{Namespace: namespace, PodName: oldPrimary},
						postgres.PostgresDBName, query,
					)
					g.Expect(err).ToNot(HaveOccurred())
					GinkgoWriter.Printf("Orphaned failover slots on demoted primary:\n%s\n", out)
					// Empty result means the orphaned slot was cleaned up
					g.Expect(strings.TrimSpace(out)).To(BeEmpty(), "Expected orphaned failover slot to be cleaned up")
				}, 180).WithPolling(10 * time.Second).Should(Succeed())
			})
		})
	})
})
