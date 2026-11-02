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
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin counterparts of the in-core object-store backup scenarios that go
// beyond a plain backup/restore round-trip: scheduled backups, choosing the
// backup target, and point-in-time recovery. They share one cluster archiving
// through plugin-barman-cloud; the in-core variants are left in place.
var _ = Describe("plugin-barman-cloud scheduled backups, standby target and PITR",
	Label(tests.LabelPluginBarmanCloud, tests.LabelBackupRestore), func() {
		const (
			clusterManifest       = fixturesDir + "/plugin_barman_cloud/cluster-plugin-backup-features.yaml.template"
			scheduledManifest     = fixturesDir + "/plugin_barman_cloud/scheduled-backup-immediate-plugin.yaml"
			standbyBackupManifest = fixturesDir + "/plugin_barman_cloud/backup-plugin-standby.yaml"
			// Matches metadata.name of the cluster fixture and the barmanObjectName
			// parameter it references.
			clusterName         = "cluster-plugin-backup"
			scheduledBackupName = "scheduled-backup-plugin-immediate"
			pluginName          = "barman-cloud.cloudnative-pg.io"
			pitrTable           = "for_restore"
			pitrRestoredCluster = "cluster-plugin-backup-pitr"
			level               = tests.High
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		Context("on a cluster archiving through the plugin", Ordered, func() {
			var namespace string

			BeforeAll(func() {
				const namespacePrefix = "plugin-backup-features"
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				setupPluginObjectStore(namespace, clusterName)

				By("creating a cluster that archives through the plugin", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
					backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 120)
				})
			})

			It("runs a scheduled backup through the plugin", func() {
				backupasserts.AssertScheduledBackupsImmediate(env, namespace, scheduledManifest, scheduledBackupName)
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)
				By("verifying the base backup is on the object store", func() {
					latestTar := objectstore.GetFilePath(clusterName, "data.tar")
					Eventually(func() (int, error) {
						return objectstore.CountFiles(objectStoreEnv, latestTar)
					}, 60).Should(BeEquivalentTo(1))
				})
			})

			It("backs up from a standby through the plugin", func() {
				By("taking a backup with a prefer-standby target", func() {
					// onlyTargetStandbys makes Execute assert the backup ran on
					// an instance other than the primary.
					backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, standbyBackupManifest, true,
						testTimeouts[timeouts.BackupIsReady])
				})

				By("verifying the base backup is on the object store", func() {
					latestTar := objectstore.GetFilePath(clusterName, "data.tar")
					Eventually(func() (int, error) {
						return objectstore.CountFiles(objectStoreEnv, latestTar)
					}, 60).Should(BeEquivalentTo(2))
				})
			})

			It("restores to a point in time from a plugin backup", func() {
				// The base backups of the previous specs predate this table, so the
				// restore below replays its creation and the first two rows from the
				// archived WAL, then stops at the recovery target before the third.
				pgasserts.AssertCreateTestData(env, pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    pitrTable,
				})

				var targetTime string
				By("getting the recovery target timestamp", func() {
					ts, err := postgres.GetCurrentTimestamp(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						namespace, clusterName,
					)
					Expect(err).ToNot(HaveOccurred())
					targetTime = ts
				})

				By("writing a row beyond the recovery target", func() {
					forward, conn, err := postgres.ForwardPSQLConnection(
						env.Ctx,
						env.Client,
						env.Interface,
						env.RestClientConfig,
						namespace,
						clusterName,
						postgres.AppDBName,
						apiv1.ApplicationUserSecretSuffix,
					)
					defer func() {
						_ = conn.Close()
						forward.Close()
					}()
					Expect(err).ToNot(HaveOccurred())
					pgasserts.InsertRecordIntoTable(pitrTable, 3, conn)
				})

				By("archiving the WAL holding the recovery target", func() {
					objectstoreasserts.AssertArchiveWalOnObjectStore(
						env, testTimeouts, objectStoreEnv, namespace, clusterName, clusterName)
				})

				By("restoring into a new cluster up to the recovery target", func() {
					_, err := backups.CreateClusterFromExternalClusterBackupWithPITRUsingPlugin(
						env.Ctx, env.Client,
						namespace, pitrRestoredCluster, clusterName, pluginName, targetTime)
					Expect(err).ToNot(HaveOccurred())
					clusterasserts.AssertClusterIsReady(env, namespace, pitrRestoredCluster,
						testTimeouts[timeouts.ClusterIsReady])
				})

				backupasserts.AssertClusterWasRestoredWithPITRAndApplicationDB(env, testTimeouts,
					namespace, pitrRestoredCluster, pitrTable, "00000002")
			})
		})
	})
