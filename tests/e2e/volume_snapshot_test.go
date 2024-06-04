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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

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
		) (volumesnapshot.VolumeSnapshotList, error) {
			var snapshotList volumesnapshot.VolumeSnapshotList
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

		// Initializing a global namespace variable to be used in each test case
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
				clusterName, err = env.GetResourceNameFromYAML(sampleFile)
				Expect(err).ToNot(HaveOccurred())

				// Initializing namespace variable to be used in test case
				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				DeferCleanup(func() error {
					if CurrentSpecReport().Failed() {
						env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
					}
					return env.DeleteNamespace(namespace)
				})

				// Creating a cluster with three nodes
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
			})

			It("can create a Volume Snapshot", func() {
				var backupObject apiv1.Backup
				By("creating a volumeSnapshot and waiting until it's completed", func() {
					err := testUtils.CreateOnDemandBackupViaKubectlPlugin(
						namespace,
						clusterName,
						"",
						apiv1.BackupTargetStandby,
						apiv1.BackupMethodVolumeSnapshot,
					)
					Expect(err).ToNot(HaveOccurred())

					// trigger a checkpoint as the backup may run on standby
					CheckPointAndSwitchWalOnPrimary(namespace, clusterName)
					Eventually(func(g Gomega) {
						backupList, err := env.GetBackupList(namespace)
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
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("checking that volumeSnapshots are properly labeled", func() {
					Eventually(func(g Gomega) {
						for _, snapshot := range backupObject.Status.BackupSnapshotStatus.Elements {
							volumeSnapshot, err := env.GetVolumeSnapshot(namespace, snapshot.Name)
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

				if !(IsLocal() || IsGKE()) {
					Skip("This test is only executed on gke, openshift and local")
				}

				var err error
				clusterToSnapshotName, err = env.GetResourceNameFromYAML(clusterToSnapshot)
				Expect(err).ToNot(HaveOccurred())

				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				DeferCleanup(func() error {
					if CurrentSpecReport().Failed() {
						env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
					}
					return env.DeleteNamespace(namespace)
				})

				By("create the certificates for MinIO", func() {
					err := minioEnv.CreateCaSecret(env, namespace)
					Expect(err).ToNot(HaveOccurred())
				})

				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
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

				By("verify test connectivity to minio using barman-cloud-wal-archive script", func() {
					primaryPod, err := env.GetClusterPrimary(namespace, clusterToSnapshotName)
					Expect(err).ToNot(HaveOccurred())
					Eventually(func() (bool, error) {
						connectionStatus, err := testUtils.MinioTestConnectivityUsingBarmanCloudWalArchive(
							namespace, clusterToSnapshotName, primaryPod.GetName(), "minio", "minio123", minioEnv.ServiceName)
						if err != nil {
							return false, err
						}
						return connectionStatus, nil
					}, 60).Should(BeTrue())
				})

				var backup *apiv1.Backup
				By("creating a snapshot and waiting until it's completed", func() {
					var err error
					backupName := fmt.Sprintf("%s-example", clusterToSnapshotName)
					backup, err = testUtils.CreateOnDemandBackup(
						namespace,
						clusterToSnapshotName,
						backupName,
						apiv1.BackupTargetStandby,
						apiv1.BackupMethodVolumeSnapshot,
						env)
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
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("fetching the volume snapshots", func() {
					snapshotList, err := getSnapshots(backup.Name, clusterToSnapshotName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))

					envVars := testUtils.EnvVarsForSnapshots{
						DataSnapshot: snapshotDataEnv,
						WalSnapshot:  snapshotWalEnv,
					}
					err = testUtils.SetSnapshotNameAsEnv(&snapshotList, backup, envVars)
					Expect(err).ToNot(HaveOccurred())
				})

				By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
					// Create a "test" table with values 1,2
					AssertCreateTestData(namespace, clusterToSnapshotName, tableName, psqlClientPod)

					// Because GetCurrentTimestamp() rounds down to the second and is executed
					// right after the creation of the test data, we wait for 1s to avoid not
					// including the newly created data within the recovery_target_time
					time.Sleep(1 * time.Second)
					// Get the recovery_target_time and pass it to the template engine
					recoveryTargetTime, err := testUtils.GetCurrentTimestamp(namespace, clusterToSnapshotName, env, psqlClientPod)
					Expect(err).ToNot(HaveOccurred())
					err = os.Setenv(recoveryTargetTimeEnv, recoveryTargetTime)
					Expect(err).ToNot(HaveOccurred())

					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(namespace, clusterToSnapshotName, tableName, 3, psqlClientPod)
					insertRecordIntoTable(namespace, clusterToSnapshotName, tableName, 4, psqlClientPod)

					// Close and archive the current WAL file
					AssertArchiveWalOnMinio(namespace, clusterToSnapshotName, clusterToSnapshotName)
				})

				clusterToRestoreName, err := env.GetResourceNameFromYAML(clusterSnapshotRestoreFile)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot and PITR", func() {
					AssertCreateCluster(namespace, clusterToRestoreName, clusterSnapshotRestoreFile, env)
					AssertClusterIsReady(namespace, clusterToRestoreName, testTimeouts[testUtils.ClusterIsReadySlow], env)
				})

				By("verifying the correct data exists in the restored cluster", func() {
					restoredPrimary, err := env.GetClusterPrimary(namespace, clusterToRestoreName)
					Expect(err).ToNot(HaveOccurred())
					AssertDataExpectedCount(namespace, clusterToRestoreName, tableName, 2, restoredPrimary)
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
			) volumesnapshot.VolumeSnapshotList {
				snapshotList := volumesnapshot.VolumeSnapshotList{}
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

				if !(IsLocal() || IsGKE()) {
					Skip("This test is only executed on gke, openshift and local")
				}

				var err error
				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() error {
					if CurrentSpecReport().Failed() {
						env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
					}
					if err := os.Unsetenv(snapshotDataEnv); err != nil {
						return err
					}
					if err := os.Unsetenv(snapshotWalEnv); err != nil {
						return err
					}
					return env.DeleteNamespace(namespace)
				})
				clusterToBackupName, err = env.GetResourceNameFromYAML(clusterToBackupFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster on which to execute the backup", func() {
					AssertCreateCluster(namespace, clusterToBackupName, clusterToBackupFilePath, env)
				})
			})

			It("can create a declarative cold backup and restoring using it", func() {
				By("inserting test data", func() {
					AssertCreateTestData(namespace, clusterToBackupName, tableName, psqlClientPod)
				})

				backupName, err := env.GetResourceNameFromYAML(backupFileFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the backup", func() {
					err := CreateResourcesFromFileWithError(namespace, backupFileFilePath)
					Expect(err).ToNot(HaveOccurred())
				})

				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace}, &backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
					AssertBackupConditionInClusterStatus(namespace, clusterToBackupName)
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
					clusterToBackup, err = env.GetCluster(namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				snapshotList := getAndVerifySnapshots(clusterToBackup, backup)
				envVars := testUtils.EnvVarsForSnapshots{
					DataSnapshot: snapshotDataEnv,
					WalSnapshot:  snapshotWalEnv,
				}
				err = testUtils.SetSnapshotNameAsEnv(&snapshotList, &backup, envVars)
				Expect(err).ToNot(HaveOccurred())

				clusterToRestoreName, err := env.GetResourceNameFromYAML(clusterToRestoreFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the restore", func() {
					CreateResourceFromFile(namespace, clusterToRestoreFilePath)
				})

				By("checking that the data is present on the restored cluster", func() {
					AssertDataExpectedCount(namespace, clusterToRestoreName, tableName, 2, psqlClientPod)
				})
			})
			It("can take a snapshot targeting the primary", func() {
				backupName, err := env.GetResourceNameFromYAML(backupPrimaryFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the backup", func() {
					err := CreateResourcesFromFileWithError(namespace, backupPrimaryFilePath)
					Expect(err).ToNot(HaveOccurred())
				})

				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace}, &backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(
							BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
					AssertBackupConditionInClusterStatus(namespace, clusterToBackupName)
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
					clusterToBackup, err = env.GetCluster(namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				_ = getAndVerifySnapshots(clusterToBackup, backup)

				By("ensuring cluster resumes after snapshot", func() {
					AssertClusterIsReady(namespace, clusterToBackupName, testTimeouts[testUtils.ClusterIsReadyQuick], env)
				})
			})

			It("can take a snapshot in a single instance cluster", func() {
				By("scaling down the cluster to a single instance", func() {
					cluster, err := env.GetCluster(namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())

					updated := cluster.DeepCopy()
					updated.Spec.Instances = 1
					err = env.Client.Patch(env.Ctx, updated, k8client.MergeFrom(cluster))
					Expect(err).ToNot(HaveOccurred())
				})

				By("ensuring there is only one pod", func() {
					Eventually(func(g Gomega) {
						pods, err := env.GetClusterPodList(namespace, clusterToBackupName)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(pods.Items).To(HaveLen(1))
					}, testTimeouts[testUtils.ClusterIsReadyQuick]).Should(Succeed())
				})

				backupName := "single-instance-snap"
				By("taking a backup snapshot", func() {
					_, err := testUtils.CreateOnDemandBackup(
						namespace,
						clusterToBackupName,
						backupName,
						apiv1.BackupTargetStandby,
						apiv1.BackupMethodVolumeSnapshot,
						env)
					Expect(err).NotTo(HaveOccurred())
				})

				CheckPointAndSwitchWalOnPrimary(namespace, clusterToBackupName)
				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace}, &backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
					AssertBackupConditionInClusterStatus(namespace, clusterToBackupName)
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
					clusterToBackup, err = env.GetCluster(namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				_ = getAndVerifySnapshots(clusterToBackup, backup)

				By("ensuring cluster resumes after snapshot", func() {
					AssertClusterIsReady(namespace, clusterToBackupName, testTimeouts[testUtils.ClusterIsReadyQuick], env)
				})
			})
		})

		Context("Declarative Hot Backup", Ordered, func() {
			// test env constants
			const (
				namespacePrefix = "volume-snapshot-recovery"
				level           = tests.High
				filesDir        = fixturesDir + "/volume_snapshot"
				snapshotDataEnv = "SNAPSHOT_PITR_PGDATA"
				snapshotWalEnv  = "SNAPSHOT_PITR_PGWAL"
			)
			// file constants
			const (
				clusterToSnapshot = filesDir + "/cluster-pvc-hot-snapshot.yaml.template"
			)

			var clusterToSnapshotName string
			BeforeAll(func() {
				if testLevelEnv.Depth < int(level) {
					Skip("Test depth is lower than the amount requested for this test")
				}

				if !(IsLocal() || IsGKE()) {
					Skip("This test is only executed on gke, openshift and local")
				}

				var err error
				clusterToSnapshotName, err = env.GetResourceNameFromYAML(clusterToSnapshot)
				Expect(err).ToNot(HaveOccurred())

				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				DeferCleanup(func() error {
					if CurrentSpecReport().Failed() {
						env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
					}
					return env.DeleteNamespace(namespace)
				})

				By("create the certificates for MinIO", func() {
					err := minioEnv.CreateCaSecret(env, namespace)
					Expect(err).ToNot(HaveOccurred())
				})

				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")

				By("creating the cluster to snapshot", func() {
					AssertCreateCluster(namespace, clusterToSnapshotName, clusterToSnapshot, env)
				})

				By("verify test connectivity to minio using barman-cloud-wal-archive script", func() {
					primaryPod, err := env.GetClusterPrimary(namespace, clusterToSnapshotName)
					Expect(err).ToNot(HaveOccurred())
					Eventually(func() (bool, error) {
						connectionStatus, err := testUtils.MinioTestConnectivityUsingBarmanCloudWalArchive(
							namespace, clusterToSnapshotName, primaryPod.GetName(), "minio", "minio123", minioEnv.ServiceName)
						if err != nil {
							return false, err
						}
						return connectionStatus, nil
					}, 60).Should(BeTrue())
				})
			})

			It("should execute a backup with online set to true", func() {
				const (
					tableName                  = "online_test"
					clusterSnapshotRestoreFile = filesDir + "/cluster-pvc-hot-restore.yaml.template"
				)

				DeferCleanup(func() error {
					if err := os.Unsetenv(snapshotDataEnv); err != nil {
						return err
					}

					return os.Unsetenv(snapshotWalEnv)
				})

				By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
					// Create a "test" table with values 1,2
					AssertCreateTestData(namespace, clusterToSnapshotName, tableName, psqlClientPod)

					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(namespace, clusterToSnapshotName, tableName, 3, psqlClientPod)
					insertRecordIntoTable(namespace, clusterToSnapshotName, tableName, 4, psqlClientPod)

					// Close and archive the current WAL file
					AssertArchiveWalOnMinio(namespace, clusterToSnapshotName, clusterToSnapshotName)
				})

				var backup *apiv1.Backup
				By("creating a snapshot and waiting until it's completed", func() {
					var err error
					backupName := fmt.Sprintf("%s-online", clusterToSnapshotName)
					backup, err = testUtils.CreateBackup(
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
						env,
					)
					Expect(err).ToNot(HaveOccurred())

					Eventually(func(g Gomega) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      backupName,
						}, backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
						g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
						g.Expect(backup.Status.BackupLabelFile).ToNot(BeEmpty())
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("fetching the volume snapshots", func() {
					snapshotList, err := getSnapshots(backup.Name, clusterToSnapshotName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))

					envVars := testUtils.EnvVarsForSnapshots{
						DataSnapshot: snapshotDataEnv,
						WalSnapshot:  snapshotWalEnv,
					}
					err = testUtils.SetSnapshotNameAsEnv(&snapshotList, backup, envVars)
					Expect(err).ToNot(HaveOccurred())
				})

				clusterToRestoreName, err := env.GetResourceNameFromYAML(clusterSnapshotRestoreFile)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot and PITR", func() {
					AssertCreateCluster(namespace, clusterToRestoreName, clusterSnapshotRestoreFile, env)
					AssertClusterIsReady(namespace, clusterToRestoreName, testTimeouts[testUtils.ClusterIsReadySlow], env)
				})

				By("verifying the correct data exists in the restored cluster", func() {
					restoredPrimary, err := env.GetClusterPrimary(namespace, clusterToRestoreName)
					Expect(err).ToNot(HaveOccurred())
					AssertDataExpectedCount(namespace, clusterToRestoreName, tableName, 4, restoredPrimary)
				})
			})
		})
	})
