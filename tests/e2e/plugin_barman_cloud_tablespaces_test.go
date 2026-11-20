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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/config"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

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
			tbsFixtures         = fixturesDir + "/tablespaces/plugin-barman-cloud"
			srcManifest         = tbsFixtures + "/cluster-with-tablespaces-plugin.yaml.template"
			restoreManifest     = tbsFixtures + "/restore-cluster-from-plugin-tablespaces.yaml.template"
			backupManifest      = tbsFixtures + "/backup-cluster-tablespaces-plugin.yaml"
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

// Plugin counterpart of the in-core "tablespaces and volumesnapshot support"
// scenarios: an online volume snapshot backup of a cluster with tablespaces,
// whose WALs are archived (and fetched during recovery) through
// plugin-barman-cloud, is restored both as-is and with point-in-time recovery.
var _ = Describe("plugin-barman-cloud tablespaces volume snapshot backup and restore",
	Label(tests.LabelPluginBarmanCloud, tests.LabelTablespaces, tests.LabelSnapshot, tests.LabelBackupRestore),
	Ordered, func() {
		const (
			tbsFixtures     = fixturesDir + "/tablespaces/plugin-barman-cloud"
			clusterManifest = tbsFixtures + "/cluster-volume-snapshot-tablespaces-plugin.yaml.template"
			backupManifest  = tbsFixtures + "/backup-volume-snapshot-tablespaces-plugin.yaml"
			restoreManifest = tbsFixtures + "/cluster-volume-snapshot-tablespaces-restore-plugin.yaml.template"
			pitrManifest    = tbsFixtures + "/cluster-volume-snapshot-tablespaces-pitr-plugin.yaml.template"
			clusterName     = "cluster-tablespaces-volume-snapshot-plugin"
			level           = tests.Medium

			snapshotDataEnv       = "SNAPSHOT_PLUGIN_TBS_PITR_PGDATA"
			snapshotWalEnv        = "SNAPSHOT_PLUGIN_TBS_PITR_PGWAL"
			snapshotTbsEnv        = "SNAPSHOT_PLUGIN_TBS_PITR_PGTABLESPACE"
			recoveryTargetTimeEnv = "SNAPSHOT_PLUGIN_TBS_PITR"

			tablespace1 = "tbs1"
			table1      = "test_tbs1"
			tablespace2 = "tbs2"
			table2      = "test_tbs2"
		)

		var (
			namespace    string
			backupName   string
			backupObject *apiv1.Backup
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		BeforeAll(func() {
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "plugin-tablespaces-snapshot")
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, clusterName)

			By("creating the source cluster with tablespaces archiving through the plugin", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
			})
		})

		It("has the tablespaces mounted and created on the source cluster", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.Short])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.Short])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.Short])
		})

		It("creates a volume snapshot backup of the cluster with tablespaces", func() {
			By("writing data into both tablespaces", func() {
				backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 120)
				pgasserts.AssertCreateTestData(env, pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    table1,
					Tablespace:   tablespace1,
				})
				pgasserts.AssertCreateTestData(env, pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    table2,
					Tablespace:   tablespace2,
				})
				objectstoreasserts.CheckPointAndSwitchWalOnPrimary(env, namespace, clusterName)
			})

			var err error
			backupName, err = yaml.GetResourceNameFromYAML(env.Scheme, backupManifest)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("creating backup %s and waiting for it to complete", backupName), func() {
				backupObject = backups.Execute(env.Ctx, env.Client, env.Scheme,
					namespace, backupManifest, false,
					testTimeouts[timeouts.VolumeSnapshotIsReady])
			})
			backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)

			By("checking that volumeSnapshots are properly labeled", func() {
				assertVolumeSnapshotsAreLabeled(namespace, clusterName, backupObject)
				// 3 snapshots: PGDATA and the 2 tablespaces, the WAL volume is skipped.
				Expect(backupObject.Status.BackupSnapshotStatus.Elements).To(HaveLen(3))
			})

			assertArchiveWalClosingPluginBackup(namespace, clusterName)
		})

		It("restores a cluster from the volume snapshot backup", func() {
			config.SetTemplateVariable("BACKUP_NAME", backupName)
			restoredName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreManifest)
			Expect(err).ToNot(HaveOccurred())

			By("creating the cluster to be restored through snapshot", func() {
				resources.CreateResourceFromFile(env, namespace, restoreManifest)
				clusterasserts.AssertClusterIsReady(env, namespace, restoredName,
					testTimeouts[timeouts.ClusterIsReadySlow])
			})

			assertTablespacesRestoredCluster(namespace, restoredName, 2, table1, table2)
		})

		It("recovers from the volume snapshot backup with PITR", func() {
			By("inserting more rows and picking the recovery target time", func() {
				forward, conn, err := postgres.ForwardPSQLConnection(env.Ctx, env.Client, env.Interface,
					env.RestClientConfig, namespace, clusterName, postgres.AppDBName,
					apiv1.ApplicationUserSecretSuffix)
				Expect(err).ToNot(HaveOccurred())
				defer func() {
					_ = conn.Close()
					forward.Close()
				}()

				// Rows 3 and 4 must be present after the recovery
				for _, table := range []string{table1, table2} {
					pgasserts.InsertRecordIntoTable(table, 3, conn)
					pgasserts.InsertRecordIntoTable(table, 4, conn)
				}

				// GetCurrentTimestamp rounds down to the second: wait to avoid
				// excluding the rows above from the recovery target.
				time.Sleep(1 * time.Second)
				recoveryTargetTime, err := postgres.GetCurrentTimestamp(env.Ctx, env.Client, env.Interface,
					env.RestClientConfig, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				config.SetTemplateVariable(recoveryTargetTimeEnv, recoveryTargetTime)

				// Rows 5 and 6 must not be present after the recovery
				for _, table := range []string{table1, table2} {
					pgasserts.InsertRecordIntoTable(table, 5, conn)
					pgasserts.InsertRecordIntoTable(table, 6, conn)
				}
			})

			By("archiving the current WAL", func() {
				objectstoreasserts.AssertArchiveWalOnObjectStore(env, testTimeouts, objectStoreEnv,
					namespace, clusterName, clusterName)
			})

			By("exporting the volume snapshot names to the recovery manifest", func() {
				snapshotList, err := getSnapshots(backupName, clusterName, namespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(snapshotList.Items).To(HaveLen(len(backupObject.Status.BackupSnapshotStatus.Elements)))
				Expect(storage.SetSnapshotTemplateVariables(&snapshotList, backupObject,
					storage.SnapshotTemplateVariables{
						DataSnapshot:             snapshotDataEnv,
						WalSnapshot:              snapshotWalEnv,
						TablespaceSnapshotPrefix: snapshotTbsEnv,
					})).To(Succeed())
			})

			pitrClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, pitrManifest)
			Expect(err).ToNot(HaveOccurred())

			By("creating the cluster recovered with PITR", func() {
				resources.CreateResourceFromFile(env, namespace, pitrManifest)
				clusterasserts.AssertClusterIsReady(env, namespace, pitrClusterName,
					testTimeouts[timeouts.ClusterIsReadySlow])
			})

			assertTablespacesRestoredCluster(namespace, pitrClusterName, 4, table1, table2)
		})
	})
