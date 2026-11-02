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
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin counterpart of the object-store tablespaces backup/restore scenario:
// a cluster with tablespaces archives through plugin-barman-cloud, is backed up
// through the plugin, and is restored into a new cluster whose tablespaces are
// recreated from the backup. It verifies tablespaces survive a plugin
// backup/restore round-trip; the in-core variant (which shares its cluster with
// owner/temporary-tablespace and volume-snapshot sub-tests) is left in place.
var _ = Describe("plugin-barman-cloud tablespaces backup and restore",
	Label(tests.LabelPluginBarmanCloud, tests.LabelTablespaces, tests.LabelBackupRestore), func() {
		const (
			srcManifest         = fixturesDir + "/tablespaces/cluster-with-tablespaces-plugin.yaml.template"
			restoreManifest     = fixturesDir + "/tablespaces/restore-cluster-from-plugin-tablespaces.yaml.template"
			backupManifest      = fixturesDir + "/tablespaces/backup-cluster-tablespaces-plugin.yaml"
			srcClusterName      = "cluster-tablespaces-plugin"
			restoredClusterName = "cluster-restore-tablespaces-plugin"
			numTablespaces      = 2
			testTablespace      = "atablespace"
			testTableName       = "tbs_restore"
			level               = tests.High
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		It("backs up and restores a cluster with tablespaces through the plugin", func() {
			const namespacePrefix = "plugin-tablespaces-backup"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, srcClusterName)

			By("creating the source cluster with tablespaces archiving through the plugin", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, srcClusterName, srcManifest)
			})

			By("writing data into a tablespace once WAL archiving is working", func() {
				// Waiting for ContinuousArchiving and writing data (which generates
				// WAL) ensures the WAL needed to reach a consistent recovery point
				// is archived, so the standalone restore below can complete.
				backupasserts.AssertArchiveConditionMet(env, namespace, srcClusterName, 120)
				pgasserts.AssertCreateTestData(env, pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  srcClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    testTableName,
					Tablespace:   testTablespace,
				})
			})

			By("backing up the source cluster through the plugin", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupManifest, false,
					testTimeouts[timeouts.BackupIsReady])
			})

			assertArchiveWalClosingPluginBackup(namespace, srcClusterName)

			By("restoring into a new cluster with tablespaces through the plugin", func() {
				resources.CreateResourceFromFile(env, namespace, restoreManifest)
				// Restoring a cluster with tablespaces can take noticeably longer.
				clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName,
					testTimeouts[timeouts.ClusterIsReadySlow])
			})

			By("verifying the restored cluster has the tablespaces", func() {
				restoredCluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, restoredClusterName)
				Expect(err).ToNot(HaveOccurred())
				AssertClusterHasMountPointsAndVolumesForTablespaces(restoredCluster, numTablespaces,
					testTimeouts[timeouts.Short])
				AssertClusterHasPvcsAndDataDirsForTablespaces(restoredCluster, testTimeouts[timeouts.Short])
				AssertDatabaseContainsTablespaces(restoredCluster, testTimeouts[timeouts.Short])

				// Confirm the data stored in the tablespace was actually restored
				// from the backup, not just that the tablespace exists.
				pgasserts.AssertDataExpectedCount(env, pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  restoredClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    testTableName,
					Tablespace:   testTablespace,
				}, 2)
			})
		})
	})
