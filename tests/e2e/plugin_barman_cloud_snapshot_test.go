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
	"os"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin counterparts of the in-core volume-snapshot scenarios: the data base
// backup is still taken with the native volumeSnapshot method, but WAL archiving
// (and the WAL source used during recovery) goes through plugin-barman-cloud
// rather than the deprecated in-core barmanObjectStore. The in-core variants are
// left in place. Runs on kind/k3d only, where the plugin and the shared object
// store are installed.
var _ = Describe("plugin-barman-cloud volume snapshots",
	Label(tests.LabelPluginBarmanCloud, tests.LabelSnapshot, tests.LabelBackupRestore), func() {
		const level = tests.High

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
		})

		// getSnapshots lists the VolumeSnapshots produced by a given backup of a
		// cluster, so their names can be fed to the recovery manifests.
		getSnapshots := func(
			backupName string,
			clusterName string,
			namespace string,
		) (volumesnapshotv1.VolumeSnapshotList, error) {
			var snapshotList volumesnapshotv1.VolumeSnapshotList
			err := env.Client.List(env.Ctx, &snapshotList, k8client.InNamespace(namespace),
				k8client.MatchingLabels{
					utils.ClusterLabelName:    clusterName,
					utils.BackupNameLabelName: backupName,
				})
			if err != nil {
				return snapshotList, err
			}

			return snapshotList, nil
		}

		// takeVolumeSnapshotBackup creates a volumeSnapshot-method backup of the
		// cluster on the given target and waits for it to complete with two snapshot
		// elements (PGDATA and WAL). WAL archiving is provided by plugin-barman-cloud.
		// A cold snapshot needs a checkpoint to reach a consistent stop point; an
		// online (hot) one is expected to produce a backup label file.
		takeVolumeSnapshotBackup := func(
			namespace, clusterName, backupName string,
			target apiv1.BackupTarget,
			triggerCheckpoint, requireBackupLabelFile bool,
		) *apiv1.Backup {
			GinkgoHelper()
			backup, err := backups.Create(env.Ctx, env.Client, apiv1.Backup{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: backupName},
				Spec: apiv1.BackupSpec{
					Target:  target,
					Method:  apiv1.BackupMethodVolumeSnapshot,
					Cluster: apiv1.LocalObjectReference{Name: clusterName},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			if triggerCheckpoint {
				objectstoreasserts.CheckPointAndSwitchWalOnPrimary(env, namespace, clusterName)
			}

			Eventually(func(g Gomega) {
				g.Expect(env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      backupName,
				}, backup)).To(Succeed())
				g.Expect(backup.Status.Phase).To(
					BeEquivalentTo(apiv1.BackupPhaseCompleted),
					"Backup should be completed correctly, error message is '%s'",
					backup.Status.Error)
				g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
				if requireBackupLabelFile {
					g.Expect(backup.Status.BackupLabelFile).ToNot(BeEmpty())
				}
			}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())

			return backup
		}

		// captureSnapshotNames lists the snapshots a backup produced and exports
		// their names as the given env vars, so the recovery manifests can reference
		// them through template substitution.
		captureSnapshotNames := func(backup *apiv1.Backup, clusterName, namespace, dataEnv, walEnv string) {
			GinkgoHelper()
			snapshotList, err := getSnapshots(backup.Name, clusterName, namespace)
			Expect(err).ToNot(HaveOccurred())
			Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))

			Expect(storage.SetSnapshotNameAsEnv(&snapshotList, backup, storage.EnvVarsForSnapshots{
				DataSnapshot: dataEnv,
				WalSnapshot:  walEnv,
			})).To(Succeed())
		}

		// insertDataAndArchiveWAL writes rows into tableName (optionally creating it
		// with rows 1,2 first) and forces the closing WAL to be archived through the
		// plugin, so a later recovery can reach a consistent point.
		insertDataAndArchiveWAL := func(namespace, clusterName, tableName string, createTable bool, extraRows ...int) {
			GinkgoHelper()
			if createTable {
				pgasserts.AssertCreateTestData(env, pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				})
			}

			if len(extraRows) > 0 {
				forward, conn, err := postgres.ForwardPSQLConnection(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName, postgres.AppDBName, apiv1.ApplicationUserSecretSuffix,
				)
				defer func() {
					_ = conn.Close()
					forward.Close()
				}()
				Expect(err).ToNot(HaveOccurred())
				for _, row := range extraRows {
					pgasserts.InsertRecordIntoTable(tableName, row, conn)
				}
			}

			objectstoreasserts.AssertArchiveWalOnObjectStore(
				env, testTimeouts, objectStoreEnv, namespace, clusterName, clusterName)
		}

		Context("PITR from a cold volume snapshot", Ordered, func() {
			const (
				namespacePrefix = "plugin-snapshot-recovery"
				filesDir        = fixturesDir + "/volume_snapshot"

				clusterManifest = filesDir + "/cluster-pvc-snapshot-plugin.yaml.template"
				restoreManifest = filesDir + "/cluster-pvc-snapshot-restore-plugin.yaml.template"

				snapshotDataEnv       = "SNAPSHOT_PLUGIN_PITR_PGDATA"
				snapshotWalEnv        = "SNAPSHOT_PLUGIN_PITR_PGWAL"
				recoveryTargetTimeEnv = "SNAPSHOT_PLUGIN_PITR"

				tableName = "test"
			)

			var namespace, clusterName string

			BeforeAll(func() {
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
				Expect(err).ToNot(HaveOccurred())

				setupPluginObjectStore(namespace, clusterName)
			})

			It("correctly executes PITR with a cold snapshot", func() {
				DeferCleanup(func() error {
					for _, envvar := range []string{
						snapshotDataEnv,
						snapshotWalEnv,
						recoveryTargetTimeEnv,
					} {
						if err := os.Unsetenv(envvar); err != nil {
							return err
						}
					}
					return nil
				})

				By("creating the cluster to snapshot, archiving through the plugin", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
				})

				By("verifying WAL archiving through the plugin is working", func() {
					// The cold snapshot relies on the archived WALs to reach the
					// recovery target, so fail early if the plugin cannot archive.
					backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 120)
				})

				var backup *apiv1.Backup
				By("creating a snapshot and waiting until it's completed", func() {
					backup = takeVolumeSnapshotBackup(namespace, clusterName,
						fmt.Sprintf("%s-example", clusterName), apiv1.BackupTargetStandby, true, false)
				})

				By("fetching the volume snapshots", func() {
					captureSnapshotNames(backup, clusterName, namespace, snapshotDataEnv, snapshotWalEnv)
				})

				By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
					// Create a "test" table with values 1,2
					tableLocator := pgasserts.TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					pgasserts.AssertCreateTestData(env, tableLocator)

					// Because GetCurrentTimestamp() rounds down to the second and is executed
					// right after the creation of the test data, we wait for 1s to avoid not
					// including the newly created data within the recovery_target_time
					time.Sleep(1 * time.Second)
					// Get the recovery_target_time and pass it to the template engine
					recoveryTargetTime, err := postgres.GetCurrentTimestamp(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						namespace, clusterName,
					)
					Expect(err).ToNot(HaveOccurred())
					err = os.Setenv(recoveryTargetTimeEnv, recoveryTargetTime)
					Expect(err).ToNot(HaveOccurred())

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
					// Insert 2 more rows which we expect not to be present at the end of the recovery
					pgasserts.InsertRecordIntoTable(tableName, 3, conn)
					pgasserts.InsertRecordIntoTable(tableName, 4, conn)

					// Close and archive the current WAL file
					objectstoreasserts.AssertArchiveWalOnObjectStore(
						env,
						testTimeouts,
						objectStoreEnv,
						namespace,
						clusterName,
						clusterName,
					)
				})

				// A single restore to a Postgres-timestamp recovery target fully
				// exercises volume-snapshot PITR through the plugin. The in-core
				// suite additionally covers the RFC3339 recovery-target-time format;
				// that is a core recovery_target_time parsing concern, independent of
				// the WAL archiver, so it is not duplicated here.
				clusterToRestoreName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreManifest)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot and PITR", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterToRestoreName, restoreManifest)
					clusterasserts.AssertClusterIsReady(
						env,
						namespace,
						clusterToRestoreName,
						testTimeouts[timeouts.ClusterIsReadySlow],
					)
				})

				By("verifying the correct data exists in the restored cluster", func() {
					tableLocator := pgasserts.TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
				})
			})
		})

		Context("online hot volume snapshot and scale-up", Ordered, func() {
			const (
				namespacePrefix = "plugin-snapshot-hot"
				filesDir        = fixturesDir + "/volume_snapshot"

				clusterManifest = filesDir + "/cluster-pvc-hot-snapshot-plugin.yaml.template"
				restoreManifest = filesDir + "/cluster-pvc-hot-restore-plugin.yaml.template"

				snapshotDataEnv = "SNAPSHOT_PLUGIN_HOT_PGDATA"
				snapshotWalEnv  = "SNAPSHOT_PLUGIN_HOT_PGWAL"

				tableName = "online_test"
			)

			var namespace, clusterName string
			var backupTaken *apiv1.Backup

			BeforeAll(func() {
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
				Expect(err).ToNot(HaveOccurred())

				setupPluginObjectStore(namespace, clusterName)

				By("creating the cluster to snapshot, archiving through the plugin", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
				})

				By("verifying WAL archiving through the plugin is working", func() {
					// The online backup waits for the WAL archiver to process the last
					// segment, so fail early if the plugin cannot archive.
					backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 120)
				})
			})

			It("should execute a backup with online set to true", func() {
				DeferCleanup(func() error {
					if err := os.Unsetenv(snapshotDataEnv); err != nil {
						return err
					}

					return os.Unsetenv(snapshotWalEnv)
				})

				By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
					// Create the table with rows 1,2 then add 3,4; the hot backup is
					// expected to capture them all.
					insertDataAndArchiveWAL(namespace, clusterName, tableName, true, 3, 4)
				})

				By("creating a snapshot and waiting until it's completed", func() {
					backupTaken = takeVolumeSnapshotBackup(namespace, clusterName,
						fmt.Sprintf("%s-online", clusterName), apiv1.BackupTargetPrimary, false, true)
				})

				By("fetching the volume snapshots", func() {
					captureSnapshotNames(backupTaken, clusterName, namespace, snapshotDataEnv, snapshotWalEnv)
				})

				clusterToRestoreName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreManifest)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterToRestoreName, restoreManifest)
					clusterasserts.AssertClusterIsReady(
						env, namespace, clusterToRestoreName, testTimeouts[timeouts.ClusterIsReadySlow],
					)
				})

				By("verifying the correct data exists in the restored cluster", func() {
					tableLocator := pgasserts.TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					pgasserts.AssertDataExpectedCount(env, tableLocator, 4)
				})
			})

			It("should scale up the cluster with volume snapshot", func() {
				// insert some data after the snapshot is taken, we want to verify the data exists in
				// the new pod when cluster scaled up
				By("inserting more test data and creating WALs on the cluster snapshotted", func() {
					insertDataAndArchiveWAL(namespace, clusterName, tableName, false, 5, 6)
				})

				// reuse the snapshot taken from the clusterToSnapshot cluster
				By("fetching the volume snapshots", func() {
					captureSnapshotNames(backupTaken, clusterName, namespace, snapshotDataEnv, snapshotWalEnv)
				})

				By("scale up the cluster", func() {
					err := clusterutils.ScaleSize(env.Ctx, env.Client, namespace, clusterName, 3)
					Expect(err).ToNot(HaveOccurred())
				})

				By("checking the cluster is working", func() {
					// Setting up a cluster with three pods is slow, usually 200-600s
					clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
				})

				By("checking the new replicas have been created using the snapshot", func() {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					Expect(err).ToNot(HaveOccurred())
					for _, pvc := range pvcList.Items {
						if pvc.Labels[utils.ClusterInstanceRoleLabelName] == specs.ClusterRoleLabelReplica &&
							pvc.Labels[utils.ClusterLabelName] == clusterName {
							Expect(pvc.Spec.DataSource.Kind).To(Equal(apiv1.VolumeSnapshotKind))
							Expect(pvc.Spec.DataSourceRef.Kind).To(Equal(apiv1.VolumeSnapshotKind))
						}
					}
				})

				// we need to verify the streaming replica continue works
				By("verifying the correct data exists in the new pod of the scaled cluster", func() {
					podList, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					Expect(podList.Items).To(HaveLen(2))
					tableLocator := pgasserts.TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					pgasserts.AssertDataExpectedCount(env, tableLocator, 6)
				})
			})
		})
	})
