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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test case for validating volume snapshots
// with different storage providers in different k8s environments
var _ = Describe("Verify Volume Snapshot",
	Label(tests.LabelBackupRestore, tests.LabelStorage, tests.LabelSnapshot), func() {
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

		updateClusterSnapshotClass := func(namespace, clusterName, className string) {
			cluster := &apiv1.Cluster{}
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				var err error
				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				cluster.Spec.Backup.VolumeSnapshot.ClassName = className
				return env.Client.Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())
		}

		var namespace string

		Context("using the kubectl cnpg plugin", Ordered, func() {
			const (
				sampleFile      = fixturesDir + "/volume_snapshot/cluster-volume-snapshot.yaml.template"
				namespacePrefix = "volume-snapshot"
				level           = tests.High
			)

			var clusterName string
			BeforeAll(func() {
				if testLevelEnv.Depth < int(level) {
					Skip("Test depth is lower than the amount requested for this test")
				}
				var err error
				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
				Expect(err).ToNot(HaveOccurred())

				// Initializing namespace variable to be used in test case
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// Creating a cluster with three nodes
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
			})

			It("can create a Volume Snapshot", func() {
				var backupObject apiv1.Backup
				By("creating a volumeSnapshot and waiting until it's completed", func() {
					Eventually(func() error {
						return backups.CreateOnDemandBackupViaKubectlPlugin(
							namespace,
							clusterName,
							"",
							apiv1.BackupTargetStandby,
							apiv1.BackupMethodVolumeSnapshot,
						)
					}).WithTimeout(time.Minute).WithPolling(5 * time.Second).Should(Succeed())

					// trigger a checkpoint as the backup may run on standby
					CheckPointAndSwitchWalOnPrimary(namespace, clusterName)
					Eventually(func(g Gomega) {
						backupList, err := backups.List(env.Ctx, env.Client, namespace)
						g.Expect(err).ToNot(HaveOccurred())
						for _, backup := range backupList.Items {
							if !strings.Contains(backup.Name, clusterName) {
								continue
							}
							backupObject = backup
							g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
								"Backup should be completed correctly, error message is '%s'",
								backup.Status.Error)
							g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
						}
					}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("checking that volumeSnapshots are properly labeled", func() {
					Eventually(func(g Gomega) {
						for _, snapshot := range backupObject.Status.BackupSnapshotStatus.Elements {
							volumeSnapshot, err := backups.GetVolumeSnapshot(env.Ctx, env.Client, namespace,
								snapshot.Name)
							g.Expect(err).ToNot(HaveOccurred())
							g.Expect(volumeSnapshot.Name).Should(ContainSubstring(clusterName))
							g.Expect(volumeSnapshot.Labels[utils.BackupNameLabelName]).To(BeEquivalentTo(backupObject.Name))
							g.Expect(volumeSnapshot.Labels[utils.ClusterLabelName]).To(BeEquivalentTo(clusterName))
						}
					}).Should(Succeed())
				})
			})
		})

		Context("Can restore from a Volume Snapshot", Ordered, func() {
			// test env constants
			const (
				namespacePrefix       = "volume-snapshot-recovery"
				level                 = tests.High
				filesDir              = fixturesDir + "/volume_snapshot"
				snapshotDataEnv       = "SNAPSHOT_PITR_PGDATA"
				snapshotWalEnv        = "SNAPSHOT_PITR_PGWAL"
				recoveryTargetTimeEnv = "SNAPSHOT_PITR"
			)
			// file constants
			const (
				clusterToSnapshot          = filesDir + "/cluster-pvc-snapshot.yaml.template"
				clusterSnapshotRestoreFile = filesDir + "/cluster-pvc-snapshot-restore.yaml.template"
			)
			// database constants
			const (
				tableName = "test"
			)

			var clusterToSnapshotName string
			BeforeAll(func() {
				if testLevelEnv.Depth < int(level) {
					Skip("Test depth is lower than the amount requested for this test")
				}

				var err error
				clusterToSnapshotName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterToSnapshot)
				Expect(err).ToNot(HaveOccurred())

				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				By("create the certificates for MinIO", func() {
					err := minioEnv.CreateCaSecret(env, namespace)
					Expect(err).ToNot(HaveOccurred())
				})

				_, err = secrets.CreateObjectStorageSecret(
					env.Ctx,
					env.Client,
					namespace,
					"backup-storage-creds",
					"minio",
					"minio123",
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("correctly executes PITR with a cold snapshot", func() {
				DeferCleanup(func() error {
					if err := os.Unsetenv(snapshotDataEnv); err != nil {
						return err
					}
					if err := os.Unsetenv(snapshotWalEnv); err != nil {
						return err
					}
					err := os.Unsetenv(recoveryTargetTimeEnv)
					return err
				})

				By("creating the cluster to snapshot", func() {
					AssertCreateCluster(namespace, clusterToSnapshotName, clusterToSnapshot, env)
				})

				By("verify connectivity of barman to minio", func() {
					primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterToSnapshotName)
					Expect(err).ToNot(HaveOccurred())
					Eventually(func() (bool, error) {
						connectionStatus, err := minio.TestBarmanConnectivity(
							namespace, clusterToSnapshotName, primaryPod.Name,
							"minio", "minio123", minioEnv.ServiceName)
						return connectionStatus, err
					}, 60).Should(BeTrue())
				})

				var backup *apiv1.Backup
				By("creating a snapshot and waiting until it's completed", func() {
					var err error
					backupName := fmt.Sprintf("%s-example", clusterToSnapshotName)
					backup, err = backups.Create(
						env.Ctx,
						env.Client,
						apiv1.Backup{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Name:      backupName,
							},
							Spec: apiv1.BackupSpec{
								Target:  apiv1.BackupTargetStandby,
								Method:  apiv1.BackupMethodVolumeSnapshot,
								Cluster: apiv1.LocalObjectReference{Name: clusterToSnapshotName},
							},
						},
					)
					Expect(err).ToNot(HaveOccurred())
					// trigger a checkpoint
					CheckPointAndSwitchWalOnPrimary(namespace, clusterToSnapshotName)
					Eventually(func(g Gomega) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      backupName,
						}, backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(
							BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
						g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
					}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("fetching the volume snapshots", func() {
					snapshotList, err := getSnapshots(backup.Name, clusterToSnapshotName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))

					envVars := storage.EnvVarsForSnapshots{
						DataSnapshot: snapshotDataEnv,
						WalSnapshot:  snapshotWalEnv,
					}
					err = storage.SetSnapshotNameAsEnv(&snapshotList, backup, envVars)
					Expect(err).ToNot(HaveOccurred())
				})

				By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
					// Create a "test" table with values 1,2
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToSnapshotName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertCreateTestData(env, tableLocator)

					// Because GetCurrentTimestamp() rounds down to the second and is executed
					// right after the creation of the test data, we wait for 1s to avoid not
					// including the newly created data within the recovery_target_time
					time.Sleep(1 * time.Second)
					// Get the recovery_target_time and pass it to the template engine
					recoveryTargetTime, err := postgres.GetCurrentTimestamp(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						namespace, clusterToSnapshotName,
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
						clusterToSnapshotName,
						postgres.AppDBName,
						apiv1.ApplicationUserSecretSuffix,
					)
					defer func() {
						_ = conn.Close()
						forward.Close()
					}()
					Expect(err).ToNot(HaveOccurred())
					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(tableName, 3, conn)
					insertRecordIntoTable(tableName, 4, conn)

					// Close and archive the current WAL file
					AssertArchiveWalOnMinio(namespace, clusterToSnapshotName, clusterToSnapshotName)
				})

				clusterToRestoreName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterSnapshotRestoreFile)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot and PITR", func() {
					AssertCreateCluster(namespace, clusterToRestoreName, clusterSnapshotRestoreFile, env)
					AssertClusterIsReady(namespace, clusterToRestoreName, testTimeouts[timeouts.ClusterIsReadySlow],
						env)
				})

				By("verifying the correct data exists in the restored cluster", func() {
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertDataExpectedCount(env, tableLocator, 2)
				})
			})
		})

		Context("Declarative Volume Snapshot", Ordered, func() {
			// test env constants
			const (
				namespacePrefix = "declarative-snapshot-backup"
				level           = tests.High
				filesDir        = fixturesDir + "/volume_snapshot"
				snapshotDataEnv = "SNAPSHOT_NAME_PGDATA"
				snapshotWalEnv  = "SNAPSHOT_NAME_PGWAL"
			)
			// file constants
			const (
				clusterToBackupFilePath  = filesDir + "/declarative-backup-cluster.yaml.template"
				clusterToRestoreFilePath = filesDir + "/declarative-backup-cluster-restore.yaml.template"
				backupFileFilePath       = filesDir + "/declarative-backup.yaml.template"
				backupPrimaryFilePath    = filesDir + "/declarative-backup-on-primary.yaml.template"
			)

			// database constants
			const (
				tableName = "test"
			)

			var clusterToBackupName string

			getAndVerifySnapshots := func(
				clusterToBackup *apiv1.Cluster,
				backup apiv1.Backup,
			) volumesnapshotv1.VolumeSnapshotList {
				snapshotList := volumesnapshotv1.VolumeSnapshotList{}
				By("fetching the volume snapshots", func() {
					var err error
					snapshotList, err = getSnapshots(backup.Name, clusterToBackup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))
				})

				By("ensuring that the additional labels and annotations are present", func() {
					clusterObj := &apiv1.Cluster{}
					for _, item := range snapshotList.Items {
						snapshotConfig := backup.GetVolumeSnapshotConfiguration(
							*clusterToBackup.Spec.Backup.VolumeSnapshot)
						Expect(utils.IsMapSubset(item.Annotations, snapshotConfig.Annotations)).To(BeTrue())
						Expect(utils.IsMapSubset(item.Labels, snapshotConfig.Labels)).To(BeTrue())
						Expect(item.Labels[utils.BackupNameLabelName]).To(BeEquivalentTo(backup.Name))
						Expect(item.Annotations[utils.ClusterManifestAnnotationName]).ToNot(BeEmpty())
						Expect(item.Annotations[utils.ClusterManifestAnnotationName]).To(ContainSubstring(clusterToBackupName))
						Expect(item.Annotations[utils.PgControldataAnnotationName]).ToNot(BeEmpty())
						Expect(item.Annotations[utils.PgControldataAnnotationName]).To(ContainSubstring("pg_control version number:"))
						// Ensure the ClusterManifestAnnotationName is a valid Cluster Object
						err := json.Unmarshal([]byte(item.Annotations[utils.ClusterManifestAnnotationName]), clusterObj)
						Expect(err).ToNot(HaveOccurred())
					}
				})
				return snapshotList
			}

			BeforeAll(func() {
				if testLevelEnv.Depth < int(level) {
					Skip("Test depth is lower than the amount requested for this test")
				}

				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() {
					_ = os.Unsetenv(snapshotDataEnv)
					_ = os.Unsetenv(snapshotWalEnv)
				})
				clusterToBackupName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterToBackupFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster on which to execute the backup", func() {
					AssertCreateCluster(namespace, clusterToBackupName, clusterToBackupFilePath, env)
					AssertClusterIsReady(namespace, clusterToBackupName, testTimeouts[timeouts.ClusterIsReadySlow], env)
				})
			})

			It("can create a declarative cold backup and restoring using it", func() {
				By("inserting test data", func() {
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToBackupName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertCreateTestData(env, tableLocator)
				})

				backupName, err := yaml.GetResourceNameFromYAML(env.Scheme, backupFileFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the backup", func() {
					err := CreateResourcesFromFileWithError(namespace, backupFileFilePath)
					Expect(err).ToNot(HaveOccurred())
				})

				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace},
							&backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
					}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
					backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterToBackupName)
				})

				By("checking that the backup status is correctly populated", func() {
					Expect(backup.Status.BeginWal).ToNot(BeEmpty())
					Expect(backup.Status.EndWal).ToNot(BeEmpty())
					Expect(backup.Status.BeginLSN).ToNot(BeEmpty())
					Expect(backup.Status.EndLSN).ToNot(BeEmpty())
					Expect(backup.Status.StoppedAt).ToNot(BeNil())
					Expect(backup.Status.StartedAt).ToNot(BeNil())
				})

				var clusterToBackup *apiv1.Cluster

				By("fetching the created cluster", func() {
					clusterToBackup, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				snapshotList := getAndVerifySnapshots(clusterToBackup, backup)
				envVars := storage.EnvVarsForSnapshots{
					DataSnapshot: snapshotDataEnv,
					WalSnapshot:  snapshotWalEnv,
				}
				err = storage.SetSnapshotNameAsEnv(&snapshotList, &backup, envVars)
				Expect(err).ToNot(HaveOccurred())

				clusterToRestoreName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterToRestoreFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the restore", func() {
					CreateResourceFromFile(namespace, clusterToRestoreFilePath)
					AssertClusterIsReady(namespace,
						clusterToRestoreName,
						testTimeouts[timeouts.ClusterIsReady],
						env,
					)
				})

				By("checking that the data is present on the restored cluster", func() {
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertDataExpectedCount(env, tableLocator, 2)
				})
			})
			It("can take a snapshot targeting the primary", func() {
				backupName, err := yaml.GetResourceNameFromYAML(env.Scheme, backupPrimaryFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the backup", func() {
					err := CreateResourcesFromFileWithError(namespace, backupPrimaryFilePath)
					Expect(err).ToNot(HaveOccurred())
				})

				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace},
							&backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(
							BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
					}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
					backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterToBackupName)
				})

				By("checking that the backup status is correctly populated", func() {
					Expect(backup.Status.BeginWal).ToNot(BeEmpty())
					Expect(backup.Status.EndWal).ToNot(BeEmpty())
					Expect(backup.Status.BeginLSN).ToNot(BeEmpty())
					Expect(backup.Status.EndLSN).ToNot(BeEmpty())
					Expect(backup.Status.StoppedAt).ToNot(BeNil())
					Expect(backup.Status.StartedAt).ToNot(BeNil())
				})

				var clusterToBackup *apiv1.Cluster

				By("fetching the created cluster", func() {
					clusterToBackup, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				_ = getAndVerifySnapshots(clusterToBackup, backup)

				By("ensuring cluster resumes after snapshot", func() {
					AssertClusterIsReady(namespace, clusterToBackupName, testTimeouts[timeouts.ClusterIsReadyQuick],
						env)
				})
			})

			It("can take a snapshot in a single instance cluster", func() {
				By("scaling down the cluster to a single instance", func() {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())

					updated := cluster.DeepCopy()
					updated.Spec.Instances = 1
					err = env.Client.Patch(env.Ctx, updated, k8client.MergeFrom(cluster))
					Expect(err).ToNot(HaveOccurred())
				})

				By("ensuring there is only one pod", func() {
					Eventually(func(g Gomega) {
						pods, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterToBackupName)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(pods.Items).To(HaveLen(1))
					}, testTimeouts[timeouts.ClusterIsReadyQuick]).Should(Succeed())
				})

				backupName := "single-instance-snap"
				By("taking a backup snapshot", func() {
					_, err := backups.Create(
						env.Ctx,
						env.Client,
						apiv1.Backup{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Name:      backupName,
							},
							Spec: apiv1.BackupSpec{
								Target:  apiv1.BackupTargetStandby,
								Method:  apiv1.BackupMethodVolumeSnapshot,
								Cluster: apiv1.LocalObjectReference{Name: clusterToBackupName},
							},
						},
					)
					Expect(err).NotTo(HaveOccurred())
				})

				CheckPointAndSwitchWalOnPrimary(namespace, clusterToBackupName)
				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace},
							&backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
					}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
					backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterToBackupName)
				})

				By("checking that the backup status is correctly populated", func() {
					Expect(backup.Status.BeginWal).ToNot(BeEmpty())
					Expect(backup.Status.EndWal).ToNot(BeEmpty())
					Expect(backup.Status.BeginLSN).ToNot(BeEmpty())
					Expect(backup.Status.EndLSN).ToNot(BeEmpty())
					Expect(backup.Status.StoppedAt).ToNot(BeNil())
					Expect(backup.Status.StartedAt).ToNot(BeNil())
				})

				var clusterToBackup *apiv1.Cluster
				By("fetching the created cluster", func() {
					var err error
					clusterToBackup, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				_ = getAndVerifySnapshots(clusterToBackup, backup)

				By("ensuring cluster resumes after snapshot", func() {
					AssertClusterIsReady(namespace, clusterToBackupName, testTimeouts[timeouts.ClusterIsReadyQuick],
						env)
				})
			})
		})

		Context("Declarative Hot Backup and scaleup", Ordered, func() {
			// test env constants
			const (
				namespacePrefix = "volume-snapshot-recovery"
				level           = tests.High
				filesDir        = fixturesDir + "/volume_snapshot"
				snapshotDataEnv = "SNAPSHOT_PITR_PGDATA"
				snapshotWalEnv  = "SNAPSHOT_PITR_PGWAL"
				tableName       = "online_test"
			)
			// file constants
			const (
				clusterToSnapshot          = filesDir + "/cluster-pvc-hot-snapshot.yaml.template"
				clusterSnapshotRestoreFile = filesDir + "/cluster-pvc-hot-restore.yaml.template"
			)

			var clusterToSnapshotName string
			var backupTaken *apiv1.Backup
			BeforeAll(func() {
				if testLevelEnv.Depth < int(level) {
					Skip("Test depth is lower than the amount requested for this test")
				}

				var err error
				clusterToSnapshotName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterToSnapshot)
				Expect(err).ToNot(HaveOccurred())

				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				By("create the certificates for MinIO", func() {
					err := minioEnv.CreateCaSecret(env, namespace)
					Expect(err).ToNot(HaveOccurred())
				})

				By("creating the credentials for minio", func() {
					_, err = secrets.CreateObjectStorageSecret(
						env.Ctx,
						env.Client,
						namespace,
						"backup-storage-creds",
						"minio",
						"minio123",
					)
					Expect(err).ToNot(HaveOccurred())
				})

				By("creating the cluster to snapshot", func() {
					AssertCreateCluster(namespace, clusterToSnapshotName, clusterToSnapshot, env)
				})

				By("verify connectivity of barman to minio", func() {
					primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterToSnapshotName)
					Expect(err).ToNot(HaveOccurred())
					Eventually(func() (bool, error) {
						connectionStatus, err := minio.TestBarmanConnectivity(
							namespace, clusterToSnapshotName, primaryPod.Name,
							"minio", "minio123", minioEnv.ServiceName)
						return connectionStatus, err
					}, 60).Should(BeTrue())
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
					forward, conn, err := postgres.ForwardPSQLConnection(
						env.Ctx,
						env.Client,
						env.Interface,
						env.RestClientConfig,
						namespace,
						clusterToSnapshotName,
						postgres.AppDBName,
						apiv1.ApplicationUserSecretSuffix,
					)
					defer func() {
						_ = conn.Close()
						forward.Close()
					}()
					Expect(err).ToNot(HaveOccurred())
					// Create a "test" table with values 1,2
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToSnapshotName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertCreateTestData(env, tableLocator)

					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(tableName, 3, conn)
					insertRecordIntoTable(tableName, 4, conn)

					// Close and archive the current WAL file
					AssertArchiveWalOnMinio(namespace, clusterToSnapshotName, clusterToSnapshotName)
				})

				By("creating a snapshot and waiting until it's completed", func() {
					var err error
					backupName := fmt.Sprintf("%s-online", clusterToSnapshotName)
					backupTaken, err = backups.Create(
						env.Ctx, env.Client,
						apiv1.Backup{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Name:      backupName,
							},
							Spec: apiv1.BackupSpec{
								Target:  apiv1.BackupTargetPrimary,
								Method:  apiv1.BackupMethodVolumeSnapshot,
								Cluster: apiv1.LocalObjectReference{Name: clusterToSnapshotName},
							},
						},
					)
					Expect(err).ToNot(HaveOccurred())

					Eventually(func(g Gomega) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      backupName,
						}, backupTaken)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backupTaken.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backupTaken.Status.Error)
						g.Expect(backupTaken.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
						g.Expect(backupTaken.Status.BackupLabelFile).ToNot(BeEmpty())
					}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("fetching the volume snapshots", func() {
					snapshotList, err := getSnapshots(backupTaken.Name, clusterToSnapshotName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backupTaken.Status.BackupSnapshotStatus.Elements)))

					envVars := storage.EnvVarsForSnapshots{
						DataSnapshot: snapshotDataEnv,
						WalSnapshot:  snapshotWalEnv,
					}
					err = storage.SetSnapshotNameAsEnv(&snapshotList, backupTaken, envVars)
					Expect(err).ToNot(HaveOccurred())
				})

				clusterToRestoreName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterSnapshotRestoreFile)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot and PITR", func() {
					AssertCreateCluster(namespace, clusterToRestoreName, clusterSnapshotRestoreFile, env)
					AssertClusterIsReady(namespace, clusterToRestoreName, testTimeouts[timeouts.ClusterIsReadySlow],
						env)
				})

				By("verifying the correct data exists in the restored cluster", func() {
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertDataExpectedCount(env, tableLocator, 4)
				})
			})

			It("should scale up the cluster with volume snapshot", func() {
				// insert some data after the snapshot is taken, we want to verify the data exists in
				// the new pod when cluster scaled up
				By("inserting more test data and creating WALs on the cluster snapshotted", func() {
					forward, conn, err := postgres.ForwardPSQLConnection(
						env.Ctx,
						env.Client,
						env.Interface,
						env.RestClientConfig,
						namespace,
						clusterToSnapshotName,
						postgres.AppDBName,
						apiv1.ApplicationUserSecretSuffix,
					)
					defer func() {
						_ = conn.Close()
						forward.Close()
					}()
					Expect(err).ToNot(HaveOccurred())
					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(tableName, 5, conn)
					insertRecordIntoTable(tableName, 6, conn)

					// Close and archive the current WAL file
					AssertArchiveWalOnMinio(namespace, clusterToSnapshotName, clusterToSnapshotName)
				})

				// reuse the snapshot taken from the clusterToSnapshot cluster
				By("fetching the volume snapshots", func() {
					snapshotList, err := getSnapshots(backupTaken.Name, clusterToSnapshotName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backupTaken.Status.BackupSnapshotStatus.Elements)))

					envVars := storage.EnvVarsForSnapshots{
						DataSnapshot: snapshotDataEnv,
						WalSnapshot:  snapshotWalEnv,
					}
					err = storage.SetSnapshotNameAsEnv(&snapshotList, backupTaken, envVars)
					Expect(err).ToNot(HaveOccurred())
				})

				By("scale up the cluster", func() {
					err := clusterutils.ScaleSize(env.Ctx, env.Client, namespace, clusterToSnapshotName, 3)
					Expect(err).ToNot(HaveOccurred())
				})

				By("checking the cluster is working", func() {
					// Setting up a cluster with three pods is slow, usually 200-600s
					AssertClusterIsReady(namespace, clusterToSnapshotName, testTimeouts[timeouts.ClusterIsReady], env)
				})

				By("checking the new replicas have been created using the snapshot", func() {
					pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					Expect(err).ToNot(HaveOccurred())
					for _, pvc := range pvcList.Items {
						if pvc.Labels[utils.ClusterInstanceRoleLabelName] == specs.ClusterRoleLabelReplica &&
							pvc.Labels[utils.ClusterLabelName] == clusterToSnapshotName {
							Expect(pvc.Spec.DataSource.Kind).To(Equal(apiv1.VolumeSnapshotKind))
							Expect(pvc.Spec.DataSourceRef.Kind).To(Equal(apiv1.VolumeSnapshotKind))
						}
					}
				})

				// we need to verify the streaming replica continue works
				By("verifying the correct data exists in the new pod of the scaled cluster", func() {
					podList, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace,
						clusterToSnapshotName)
					Expect(err).ToNot(HaveOccurred())
					Expect(podList.Items).To(HaveLen(2))
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToSnapshotName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertDataExpectedCount(env, tableLocator, 6)
				})
			})

			It("should clean up unused backup connections", func() {
				By("setting a non-existing snapshotClass", func() {
					updateClusterSnapshotClass(namespace, clusterToSnapshotName, "wrongSnapshotClass")
				})

				By("starting a new backup that will fail", func() {
					backupName := fmt.Sprintf("%s-failed", clusterToSnapshotName)
					failedBackup, err := backups.Create(
						env.Ctx, env.Client,
						apiv1.Backup{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Name:      backupName,
							},
							Spec: apiv1.BackupSpec{
								Target:  apiv1.BackupTargetPrimary,
								Method:  apiv1.BackupMethodVolumeSnapshot,
								Cluster: apiv1.LocalObjectReference{Name: clusterToSnapshotName},
							},
						},
					)
					Expect(err).ToNot(HaveOccurred())

					Eventually(func(g Gomega) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      backupName,
						}, failedBackup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(failedBackup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseFailed))
						g.Expect(failedBackup.Status.Error).To(ContainSubstring("Failed to get snapshot class"))
					}, RetryTimeout).Should(Succeed())
				})

				By("verifying that the backup connection is cleaned up", func() {
					primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
						clusterToSnapshotName)
					Expect(err).ToNot(HaveOccurred())
					query := "SELECT count(*) FROM pg_stat_activity WHERE query ILIKE '%pg_backup_start%' " +
						"AND application_name = 'cnpg-instance-manager'"

					Eventually(func() (int, error, error) {
						stdout, _, err := exec.QueryInInstancePod(
							env.Ctx, env.Client, env.Interface, env.RestClientConfig,
							exec.PodLocator{
								Namespace: primaryPod.Namespace,
								PodName:   primaryPod.Name,
							},
							postgres.PostgresDBName,
							query)
						value, atoiErr := strconv.Atoi(strings.TrimSpace(stdout))
						return value, err, atoiErr
					}, RetryTimeout).Should(BeEquivalentTo(0),
						"Stale backup connection should have been dropped")
				})

				By("resetting the snapshotClass value", func() {
					updateClusterSnapshotClass(namespace, clusterToSnapshotName, os.Getenv("E2E_CSI_STORAGE_CLASS"))
				})
			})
		})
	})
