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
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	replicationasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin port of the in-core "timeline divergence protection" test:
// verifies a replica refuses to adopt a timeline-history file for a
// timeline ahead of its own cluster's, from an object store shared
// with a second, already-diverged cluster.
// Runs on kind/k3d only, where the plugin and shared store are set up.
var _ = Describe("plugin-barman-cloud timeline divergence protection",
	Label(tests.LabelPluginBarmanCloud, tests.LabelBackupRestore), func() {
		const (
			level                 = tests.High
			sharedObjectStoreName = "shared-timeline"
			sharedArchiveName     = "shared-timeline-test-plugin"
			timelineFixturesDir   = fixturesDir + "/plugin_barman_cloud/timeline_divergence"
			firstClusterFile      = timelineFixturesDir + "/cluster-plugin-tl-divergence-1.yaml.template"
			secondClusterFile     = timelineFixturesDir + "/cluster-plugin-tl-divergence-2.yaml.template"
			backupFile            = timelineFixturesDir + "/backup-plugin-tl-divergence.yaml"
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
		})

		It("protects replicas from downloading future timeline history files", func() {
			const namespacePrefix = "timeline-divergence"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			firstClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, firstClusterFile)
			Expect(err).ToNot(HaveOccurred())
			secondClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, secondClusterFile)
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, sharedObjectStoreName)

			By("creating first cluster with 1 instance", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, firstClusterName, firstClusterFile)
			})

			By("verifying WAL archiving through the plugin is working on the first Cluster", func() {
				objectstoreasserts.AssertArchiveWalOnObjectStore(
					env, testTimeouts, objectStoreEnv, namespace, firstClusterName, sharedArchiveName,
				)
				backupasserts.AssertArchiveConditionMet(env, namespace, firstClusterName, 120)
			})

			By("creating backup", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupFile, false,
					testTimeouts[timeouts.BackupIsReady])
			})

			By("creating second cluster from backup", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, secondClusterName, secondClusterFile)
			})

			By("verifying second cluster is on timeline 2", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, secondClusterName)
					if err != nil {
						return 0, err
					}
					return cluster.Status.TimelineID, nil
				}, 60).Should(BeEquivalentTo(2))
			})

			By("verifying timeline 2 history file is archived", func() {
				objectstoreasserts.AssertArchiveWalOnObjectStore(
					env, testTimeouts, objectStoreEnv, namespace, secondClusterName, sharedArchiveName,
				)
				backupasserts.AssertArchiveConditionMet(env, namespace, secondClusterName, 120)
				Eventually(func() (int, error) {
					return objectstore.CountFiles(objectStoreEnv, objectstore.GetFilePath(sharedArchiveName, "00000002.history*"))
				}, 60).Should(BeNumerically(">", 0))
			})

			By("scaling first cluster to 2 instances", func() {
				err := clusterutils.ScaleSize(env.Ctx, env.Client, namespace, firstClusterName, 2)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying new replica is streaming", func() {
				// Confirms the timeline guard held: if the replica adopted the shared
				// archive's timeline-2 history file, Postgres would crash-loop on
				// "requested timeline 2 is not a child of this server's history",
				// and this streaming check would time out instead of passing.
				replicationasserts.AssertClusterStandbysAreStreaming(
					env,
					namespace,
					firstClusterName,
					testTimeouts[timeouts.ClusterIsReadyQuick],
				)
			})

			By("verifying first cluster stayed on timeline 1", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, firstClusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Status.TimelineID).To(BeEquivalentTo(1))
			})

			By("deleting the first cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, firstClusterFile)
				Expect(err).ToNot(HaveOccurred())
			})

			By("deleting the second cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, secondClusterFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
