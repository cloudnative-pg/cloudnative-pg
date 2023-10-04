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

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
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
		// Initializing a global namespace variable to be used in each test case
		var namespace string

		Context("Can create a Volume Snapshot", Ordered, func() {
			// test env constants
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

			It("using the kubectl cnpg plugin", func() {
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

					Eventually(func(g Gomega) {
						backupList, err := env.GetBackupList(namespace)
						g.Expect(err).ToNot(HaveOccurred())
						for _, backup := range backupList.Items {
							if !strings.Contains(backup.Name, clusterName) {
								continue
							}
							backupObject = backup
							g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted))
							g.Expect(backup.Status.BackupSnapshotStatus.GetSnapshots()).To(HaveLen(2))
						}
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("checking that volumeSnapshots are properly labeled", func() {
					Eventually(func(g Gomega) {
						for _, snapshot := range backupObject.Status.BackupSnapshotStatus.GetSnapshots() {
							volumeSnapshot, err := env.GetVolumeSnapshot(namespace, snapshot)
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
			// minio constants
			const (
				minioCaSecName  = "minio-server-ca-secret"
				minioTLSSecName = "minio-server-tls-secret"
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

				By("creating ca and tls certificate secrets", func() {
					// create CA certificates
					_, caPair, err := testUtils.CreateSecretCA(namespace, clusterToSnapshotName, minioCaSecName, true, env)
					Expect(err).ToNot(HaveOccurred())

					// sign and create secret using CA certificate and key
					serverPair, err := caPair.CreateAndSignPair("minio-service", certs.CertTypeServer,
						[]string{"minio-service.internal.mydomain.net, minio-service.default.svc, minio-service.default,"},
					)
					Expect(err).ToNot(HaveOccurred())
					serverSecret := serverPair.GenerateCertificateSecret(namespace, minioTLSSecName)
					err = env.Client.Create(env.Ctx, serverSecret)
					Expect(err).ToNot(HaveOccurred())
				})

				By("creating the credentials for minio", func() {
					AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
				})

				By("setting up minio", func() {
					setup, err := testUtils.MinioSSLSetup(namespace)
					Expect(err).ToNot(HaveOccurred())
					err = testUtils.InstallMinio(env, setup, uint(testTimeouts[testUtils.MinioInstallation]))
					Expect(err).ToNot(HaveOccurred())
				})

				// Create the minio client pod and wait for it to be ready.
				// We'll use it to check if everything is archived correctly
				By("setting up minio client pod", func() {
					minioClient := testUtils.MinioSSLClient(namespace)
					err := testUtils.PodCreateAndWaitForReady(env, &minioClient, 240)
					Expect(err).ToNot(HaveOccurred())
				})
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
							namespace, clusterToSnapshotName, primaryPod.GetName(), "minio", "minio123")
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

					Eventually(func(g Gomega) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      backupName,
						}, backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.BackupSnapshotStatus.GetSnapshots()).To(HaveLen(2))
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted))
					}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
				})

				By("fetching the volume snapshots", func() {
					snapshotList := volumesnapshot.VolumeSnapshotList{}
					err := env.Client.List(env.Ctx, &snapshotList, k8client.MatchingLabels{
						utils.ClusterLabelName: clusterToSnapshotName,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.GetSnapshots())))

					err = testUtils.SetSnapshotNameAsEnv(&snapshotList, snapshotDataEnv, snapshotWalEnv)
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
			)

			// database constants
			const (
				tableName = "test"
			)

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
			})

			It("creating a declarative cold backup and restoring it", func() {
				clusterToBackupName, err := env.GetResourceNameFromYAML(clusterToBackupFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster on which to execute the backup", func() {
					AssertCreateCluster(namespace, clusterToBackupName, clusterToBackupFilePath, env)
				})

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
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted))
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

				snapshotList := volumesnapshot.VolumeSnapshotList{}
				By("fetching the volume snapshots", func() {
					err := env.Client.List(env.Ctx, &snapshotList, k8client.MatchingLabels{
						utils.ClusterLabelName: clusterToBackupName,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.GetSnapshots())))
				})

				By("ensuring that the additional labels and annotations are present", func() {
					clusterObj := &apiv1.Cluster{}
					for _, item := range snapshotList.Items {
						snapshotConfig := clusterToBackup.Spec.Backup.VolumeSnapshot
						Expect(utils.IsMapSubset(item.Annotations, snapshotConfig.Annotations)).To(BeTrue())
						Expect(utils.IsMapSubset(item.Labels, snapshotConfig.Labels)).To(BeTrue())
						Expect(item.Labels[utils.BackupNameLabelName]).To(BeEquivalentTo(backup.Name))
						Expect(item.Annotations[utils.ClusterManifestAnnotationName]).ToNot(BeEmpty())
						Expect(item.Annotations[utils.ClusterManifestAnnotationName]).To(ContainSubstring(clusterToBackupName))
						Expect(item.Annotations[utils.PgControldataAnnotationName]).ToNot(BeEmpty())
						Expect(item.Annotations[utils.PgControldataAnnotationName]).To(ContainSubstring("pg_control version number:"))
						// Ensure the ClusterManifestAnnotationName is a valid Cluster Object
						err = json.Unmarshal([]byte(item.Annotations[utils.ClusterManifestAnnotationName]), clusterObj)
						Expect(err).ToNot(HaveOccurred())
					}
				})

				err = testUtils.SetSnapshotNameAsEnv(&snapshotList, snapshotDataEnv, snapshotWalEnv)
				Expect(err).ToNot(HaveOccurred())

				clusterToRestoreName, err := env.GetResourceNameFromYAML(clusterToRestoreFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the restore", func() {
					AssertCreateCluster(namespace, clusterToRestoreName, clusterToRestoreFilePath, env)
				})

				By("checking that the data is present on the restored cluster", func() {
					AssertDataExpectedCount(namespace, clusterToRestoreName, tableName, 2, psqlClientPod)
				})
			})
		})
	})
