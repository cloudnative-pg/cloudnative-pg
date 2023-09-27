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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup and restore", Label(tests.LabelBackupRestore), func() {
	const (
		level = tests.High

		azuriteBlobSampleFile = fixturesDir + "/backup/azurite/cluster-backup.yaml.template"

		tableName = "to_restore"
	)

	var namespace, clusterName, curlPodName, azStorageAccount, azStorageKey string
	currentTimestamp := new(string)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	Context("using minio as object storage for backup", Ordered, func() {
		// This is a set of tests using a minio server deployed in the same
		// namespace as the cluster. Since each cluster is installed in its
		// own namespace, they can share the configuration file

		const (
			backupFile              = fixturesDir + "/backup/minio/backup-minio.yaml"
			customQueriesSampleFile = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
			minioCaSecName          = "minio-server-ca-secret"
			minioTLSSecName         = "minio-server-tls-secret"
		)

		clusterWithMinioSampleFile := fixturesDir + "/backup/minio/cluster-with-backup-minio.yaml.template"

		BeforeAll(func() {
			//
			// IMPORTANT: this is to ensure that we test the old backup system too
			//
			if funk.RandomInt(0, 100) < 50 {
				GinkgoWriter.Println("---- Testing barman backups without the name flag ----")
				clusterWithMinioSampleFile = fixturesDir + "/backup/minio/cluster-with-backup-minio-legacy.yaml.template"
			}
			if !IsLocal() {
				Skip("This test is only run on local cluster")
			}
			const namespacePrefix = "cluster-backup-minio"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			By("creating ca and tls certificate secrets", func() {
				// create CA certificates
				_, caPair, err := testUtils.CreateSecretCA(namespace, clusterName, minioCaSecName, true, env)
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

			// Create the curl client pod and wait for it to be ready.
			By("setting up curl client pod", func() {
				curlClient := testUtils.CurlClient(namespace)
				err := testUtils.PodCreateAndWaitForReady(env, &curlClient, 240)
				Expect(err).ToNot(HaveOccurred())
				curlPodName = curlClient.GetName()
			})

			// Create ConfigMap and secrets to verify metrics for target database after backup restore
			AssertCustomMetricsResourcesExist(namespace, customQueriesSampleFile, 1, 1)

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterWithMinioSampleFile, env)

			By("verify test connectivity to minio using barman-cloud-wal-archive script", func() {
				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (bool, error) {
					connectionStatus, err := testUtils.MinioTestConnectivityUsingBarmanCloudWalArchive(
						namespace, clusterName, primaryPod.GetName(), "minio", "minio123")
					if err != nil {
						return false, err
					}
					return connectionStatus, nil
				}, 60).Should(BeTrue())
			})
		})

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restores a cluster using minio", func() {
			const (
				targetDBOne              = "test"
				targetDBTwo              = "test1"
				targetDBSecret           = "secret_test"
				testTableName            = "test_table"
				clusterRestoreSampleFile = fixturesDir + "/backup/cluster-from-restore.yaml.template"
			)

			restoredClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			backupName, err := env.GetResourceNameFromYAML(backupFile)
			Expect(err).ToNot(HaveOccurred())
			// Create required test data
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBOne, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBTwo, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBSecret, testTableName, psqlClientPod)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)

			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)
			latestTar := minioPath(clusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster and verifying it exists on minio, backup path is %v", latestTar), func() {
				testUtils.ExecuteBackup(namespace, backupFile, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.LastSuccessfulBackup, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.LastFailedBackup, err
				}, 30).Should(BeEmpty())
			})

			By("executing a second backup and verifying the number of backups on minio", func() {
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(1))

				// delete the first backup and create a second backup
				backup := &apiv1.Backup{}
				err := env.Client.Get(env.Ctx,
					ctrlclient.ObjectKey{Namespace: namespace, Name: backupName},
					backup)
				Expect(err).ToNot(HaveOccurred())
				err = env.Client.Delete(env.Ctx, backup)
				Expect(err).ToNot(HaveOccurred())
				// create a second backup
				testUtils.ExecuteBackup(namespace, backupFile, false, testTimeouts[testUtils.BackupIsReady], env)
				latestTar = minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(2))
			})

			By("verifying the backupName is properly set in the status of the backup", func() {
				backup := &apiv1.Backup{}
				err := env.Client.Get(env.Ctx,
					ctrlclient.ObjectKey{Namespace: namespace, Name: backupName},
					backup)
				Expect(err).ToNot(HaveOccurred())
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				// We know that our current images always contain the latest barman version
				if cluster.ShouldForceLegacyBackup() {
					Expect(backup.Status.BackupName).To(BeEmpty())
				} else {
					Expect(backup.Status.BackupName).To(HavePrefix("backup-"))
				}
			})

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName, psqlClientPod)

			cluster, err := env.GetCluster(namespace, restoredClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertMetricsData(namespace, curlPodName, targetDBOne, targetDBTwo, targetDBSecret, cluster)

			previous := 0
			latestGZ := filepath.Join("*", clusterName, "*", "*.history.gz")
			By(fmt.Sprintf("checking the previous number of .history files in minio, history file name is %v",
				latestGZ), func() {
				previous, err = testUtils.CountFilesOnMinio(namespace, minioClientName, latestGZ)
				Expect(err).ToNot(HaveOccurred())
			})

			AssertSwitchover(namespace, clusterName, env)

			By("checking the number of .history after switchover", func() {
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestGZ)
				}, 60).Should(BeNumerically(">", previous))
			})

			By("deleting the restored cluster", func() {
				err = DeleteResourcesFromFile(namespace, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// We backup and restore a cluster from a standby, and verify some expected data to
		// be there
		It("backs up and restore a cluster from standby", func() {
			const (
				targetDBOne                       = "test"
				targetDBTwo                       = "test1"
				targetDBSecret                    = "secret_test"
				testTableName                     = "test_table"
				clusterWithMinioStandbySampleFile = fixturesDir + "/backup/minio/cluster-with-backup-minio-standby.yaml.template"
				backupStandbyFile                 = fixturesDir + "/backup/minio/backup-minio-standby.yaml"
			)

			targetClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioStandbySampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster with custom serverName in the backup spec
			AssertCreateCluster(namespace, targetClusterName, clusterWithMinioStandbySampleFile, env)

			// Create required test data
			AssertCreationOfTestDataForTargetDB(namespace, targetClusterName, targetDBOne, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, targetClusterName, targetDBTwo, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, targetClusterName, targetDBSecret, testTableName, psqlClientPod)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, targetClusterName, tableName, psqlClientPod)

			AssertArchiveWalOnMinio(namespace, targetClusterName, targetClusterName)
			latestTar := minioPath(targetClusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster from standby and verifying it exists on minio, backup path is %v",
				latestTar), func() {
				testUtils.ExecuteBackup(namespace, backupStandbyFile, true, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, targetClusterName)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, targetClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})
		})

		// We backup and restore a cluster from a standby, and verify some expected data to
		// be there
		It("backs up a cluster from standby with backup target defined in backup", func() {
			const (
				targetDBOne                = "test"
				targetDBTwo                = "test1"
				targetDBSecret             = "secret_test"
				testTableName              = "test_table"
				clusterWithMinioSampleFile = fixturesDir + "/backup/minio/cluster-with-backup-minio-primary.yaml.template"
				backupWithTargetFile       = fixturesDir + "/backup/minio/backup-minio-override-target.yaml"
			)

			targetClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster with custom serverName in the backup spec
			AssertCreateCluster(namespace, targetClusterName, clusterWithMinioSampleFile, env)

			// Create required test data
			AssertCreationOfTestDataForTargetDB(namespace, targetClusterName, targetDBOne, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, targetClusterName, targetDBTwo, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, targetClusterName, targetDBSecret, testTableName, psqlClientPod)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, targetClusterName, tableName, psqlClientPod)

			AssertArchiveWalOnMinio(namespace, targetClusterName, targetClusterName)
			latestTar := minioPath(targetClusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster from standby (defined in backup file) and verifying it exists on minio,"+
				" backup path is %v", latestTar), func() {
				testUtils.ExecuteBackup(namespace, backupWithTargetFile, true, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, targetClusterName)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, targetClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			By("deleting the cluster", func() {
				err = DeleteResourcesFromFile(namespace, clusterWithMinioSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// Test that the restore works if the source cluster has a custom
		// backup.barmanObjectStore.serverName that is different than the cluster name
		It("backs up and restores a cluster with custom backup serverName", func() {
			const (
				targetDBOne              = "test"
				targetDBTwo              = "test1"
				targetDBSecret           = "secret_test"
				testTableName            = "test_table"
				clusterRestoreSampleFile = fixturesDir + "/backup/cluster-from-restore-custom.yaml.template"
				// clusterWithMinioCustomSampleFile has metadata.name != backup.barmanObjectStore.serverName
				clusterWithMinioCustomSampleFile = fixturesDir +
					"/backup/minio/cluster-with-backup-minio-custom-servername.yaml.template"
				backupFileCustom  = fixturesDir + "/backup/minio/backup-minio-custom-servername.yaml"
				clusterServerName = "pg-backup-minio-Custom-Name"
			)

			customClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioCustomSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// To also dump info. from `customClusterName` cluster after this spec gets executed
			DeferCleanup(func() {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
			})

			// Create the cluster with custom serverName in the backup spec
			AssertCreateCluster(namespace, customClusterName, clusterWithMinioCustomSampleFile, env)

			// Create required test data
			AssertCreationOfTestDataForTargetDB(namespace, customClusterName, targetDBOne, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, customClusterName, targetDBTwo, testTableName, psqlClientPod)
			AssertCreationOfTestDataForTargetDB(namespace, customClusterName, targetDBSecret, testTableName, psqlClientPod)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, customClusterName, tableName, psqlClientPod)

			AssertArchiveWalOnMinio(namespace, customClusterName, clusterServerName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, backupFileCustom, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, customClusterName)
				latestBaseTar := minioPath(clusterServerName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestBaseTar)
				}, 60).Should(BeEquivalentTo(1),
					fmt.Sprintf("verify the number of backup %v is equals to 1", latestBaseTar))
				// this is the second backup we take on the bucket
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, customClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName, psqlClientPod)

			By("deleting the primary cluster", func() {
				err = DeleteResourcesFromFile(namespace, clusterWithMinioCustomSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})

			By("deleting the restored cluster", func() {
				err = DeleteResourcesFromFile(namespace, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// Create a scheduled backup with the 'immediate' option enabled. We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups 'immediate' option", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-minio.yaml"
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)
			latestBaseTar := minioPath(clusterName, "data.tar")
			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return testUtils.CountFilesOnMinio(namespace, minioClientName, latestBaseTar)
			}, 60).Should(BeNumerically(">=", 2),
				fmt.Sprintf("verify the number of backup %v is >= 2", latestBaseTar))
		})

		It("backs up and restore a cluster with PITR MinIO", func() {
			const (
				restoredClusterName = "restore-cluster-pitr-minio"
				backupFilePITR      = fixturesDir + "/backup/minio/backup-minio-pitr.yaml"
			)

			prepareClusterForPITROnMinio(
				namespace,
				clusterName,
				backupFilePITR,
				3,
				currentTimestamp,
				psqlClientPod,
			)

			cluster, err := testUtils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFilePITR,
				*currentTimestamp,
				env,
			)
			Expect(err).NotTo(HaveOccurred())

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000003", psqlClientPod)

			By("deleting the restored cluster", func() {
				Expect(testUtils.DeleteObject(env, cluster)).To(Succeed())
			})
		})

		// We create a cluster and a scheduled backup, then it is patched to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-minio.yaml"
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeNumerically(">=", 2),
					fmt.Sprintf("verify the number of backup %v is great than 2", latestTar))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})

		It("verify tags in backed files", func() {
			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)
			tags, err := testUtils.GetFileTagsOnMinio(namespace, minioClientName, "*[0-9].gz")
			Expect(err).ToNot(HaveOccurred())
			Expect(tags.Tags).ToNot(BeEmpty())

			currentPrimary, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			oldPrimary := currentPrimary.GetName()
			// Force-delete the primary
			quickDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}
			err = env.DeletePod(namespace, currentPrimary.GetName(), quickDelete)
			Expect(err).ToNot(HaveOccurred())

			AssertNewPrimary(namespace, clusterName, oldPrimary)

			tags, err = testUtils.GetFileTagsOnMinio(namespace, minioClientName, "*.history.gz")
			Expect(err).ToNot(HaveOccurred())
			Expect(tags.Tags).ToNot(BeEmpty())
		})
	})

	Context("using azure blobs as object storage with storage account access authentication", Ordered, func() {
		// We must be careful here. All the clusters use the same remote storage
		// and that means that we must use different cluster names otherwise
		// we risk mixing WALs and backups
		const azureBlobSampleFile = fixturesDir + "/backup/azure_blob/cluster-with-backup-azure-blob.yaml.template"
		const clusterRestoreSampleFile = fixturesDir + "/backup/azure_blob/cluster-from-restore.yaml.template"
		const scheduledBackupSampleFile = fixturesDir +
			"/backup/scheduled_backup_immediate/scheduled-backup-immediate-azure-blob.yaml"
		backupFile := fixturesDir + "/backup/azure_blob/backup-azure-blob.yaml"
		BeforeAll(func() {
			if !IsAKS() {
				Skip("This test is only run on AKS clusters")
			}
			azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
			azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
			const namespacePrefix = "cluster-backup-azure-blob"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(azureBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			// The Azure Blob Storage should have been created ad-hoc for the test.
			// The credentials are retrieved from the environment variables, as we can't create
			// a fixture for them
			By("creating the Azure Blob Storage credentials", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", azStorageAccount, azStorageKey)
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, azureBlobSampleFile, env)
		})

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)
			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)
			By("uploading a backup", func() {
				// We create a backup
				testUtils.ExecuteBackup(namespace, backupFile, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
				}, 30).Should(BeNumerically(">=", 1))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName, psqlClientPod)

			By("deleting the restored cluster", func() {
				err := DeleteResourcesFromFile(namespace, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// Create a scheduled backup with the 'immediate' option enabled. We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups 'immediate' option", func() {
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

			// Only one data.tar files should be present
			Eventually(func() (int, error) {
				return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 2))
		})

		It("backs up and restore a cluster with PITR", func() {
			restoredClusterName := "restore-cluster-azure-pitr"

			prepareClusterForPITROnAzureBlob(
				namespace,
				clusterName,
				backupFile,
				azStorageAccount,
				azStorageKey,
				2,
				currentTimestamp,
				psqlClientPod)

			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

			cluster, err := testUtils.CreateClusterFromBackupUsingPITR(namespace, restoredClusterName,
				backupFile, *currentTimestamp, env)
			Expect(err).ToNot(HaveOccurred())

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000002", psqlClientPod)
			By("deleting the restored cluster", func() {
				Expect(testUtils.DeleteObject(env, cluster)).To(Succeed())
			})
		})

		// We create a cluster, create a scheduled backup, patch it to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-azure-blob.yaml"
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 480)

				// AssertScheduledBackupsImmediate creates at least two backups, we should find
				// their base backups
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})
			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})
	})

	Context("using Azurite blobs as object storage", Ordered, func() {
		// This is a set of tests using an Azurite server deployed in the same
		// namespace as the cluster. Since each cluster is installed in its
		// own namespace, they can share the configuration file
		const (
			clusterRestoreSampleFile  = fixturesDir + "/backup/azurite/cluster-from-restore.yaml.template"
			scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-azurite.yaml"
			scheduledBackupImmediateSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-azurite.yaml"
			backupFile        = fixturesDir + "/backup/azurite/backup.yaml"
			azuriteCaSecName  = "azurite-ca-secret"
			azuriteTLSSecName = "azurite-tls-secret"
		)

		BeforeAll(func() {
			if !(IsLocal() || IsGKE()) {
				Skip("This test is only executed on gke, openshift and local")
			}
			const namespacePrefix = "cluster-backup-azurite"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			// Create and assert ca and tls certificate secrets on Azurite
			By("creating ca and tls certificate secrets", func() {
				err := testUtils.CreateCertificateSecretsOnAzurite(namespace, clusterName,
					azuriteCaSecName, azuriteTLSSecName, env)
				Expect(err).ToNot(HaveOccurred())
			})
			// Setup Azurite and az cli along with Postgresql cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFile, tableName, psqlClientPod)
		})

		It("restores a backed up cluster", func() {
			// Restore backup in a new cluster
			AssertClusterRestoreWithApplicationDB(namespace, clusterRestoreSampleFile, tableName, psqlClientPod)
		})

		// Create a scheduled backup with the 'immediate' option enabled.
		// We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups immediate option", func() {
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupImmediateSampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertScheduledBackupsImmediate(namespace, scheduledBackupImmediateSampleFile, scheduledBackupName)

			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return testUtils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 2))
		})

		It("backs up and restore a cluster with PITR Azurite", func() {
			const (
				restoredClusterName = "restore-cluster-pitr-azurite"
				backupFilePITR      = fixturesDir + "/backup/azurite/backup-pitr.yaml"
			)

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFilePITR, currentTimestamp, psqlClientPod)

			cluster, err := testUtils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFilePITR,
				*currentTimestamp,
				env,
			)
			Expect(err).NotTo(HaveOccurred())

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000002", psqlClientPod)

			By("deleting the restored cluster", func() {
				Expect(testUtils.DeleteObject(env, cluster)).To(Succeed())
			})
		})

		// We create a cluster, create a scheduled backup, patch it to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 3))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})
	})
})

var _ = Describe("Clusters Recovery From Barman Object Store", Label(tests.LabelBackupRestore), func() {
	const (
		fixturesBackupDir               = fixturesDir + "/backup/recovery_external_clusters/"
		azuriteBlobSampleFile           = fixturesDir + "/backup/azurite/cluster-backup.yaml.template"
		externalClusterFileMinio        = fixturesBackupDir + "external-clusters-minio-03.yaml.template"
		externalClusterFileMinioReplica = fixturesBackupDir + "external-clusters-minio-replica-04.yaml.template"
		sourceTakeFirstBackupFileMinio  = fixturesBackupDir + "backup-minio-02.yaml"
		sourceTakeSecondBackupFileMinio = fixturesBackupDir + "backup-minio-03.yaml"
		sourceTakeThirdBackupFileMinio  = fixturesBackupDir + "backup-minio-04.yaml"
		clusterSourceFileMinio          = fixturesBackupDir + "source-cluster-minio-01.yaml.template"
		sourceBackupFileAzure           = fixturesBackupDir + "backup-azure-blob-02.yaml"
		clusterSourceFileAzure          = fixturesBackupDir + "source-cluster-azure-blob-01.yaml.template"
		externalClusterFileAzure        = fixturesBackupDir + "external-clusters-azure-blob-03.yaml.template"
		sourceBackupFileAzurePITR       = fixturesBackupDir + "backup-azure-blob-pitr.yaml"
		externalClusterFileAzurite      = fixturesBackupDir + "external-clusters-azurite-03.yaml.template"
		backupFileAzurite               = fixturesBackupDir + "backup-azurite-02.yaml"
		tableName                       = "to_restore"
		clusterSourceFileAzureSAS       = fixturesBackupDir + "cluster-with-backup-azure-blob-sas.yaml.template"
		clusterRestoreFileAzureSAS      = fixturesBackupDir + "cluster-from-restore-sas.yaml.template"
		sourceBackupFileAzureSAS        = fixturesBackupDir + "backup-azure-blob-sas.yaml"
		sourceBackupFileAzurePITRSAS    = fixturesBackupDir + "backup-azure-blob-pitr-sas.yaml"
		level                           = tests.High
		minioCaSecName                  = "minio-server-ca-secret"
		minioTLSSecName                 = "minio-server-tls-secret"
		azuriteCaSecName                = "azurite-ca-secret"
		azuriteTLSSecName               = "azurite-tls-secret"
	)

	var namespace, clusterName, azStorageAccount, azStorageKey string
	currentTimestamp := new(string)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	// Restore cluster using a recovery object store, that is a backup of another cluster,
	// created by Barman Cloud, and defined via the barmanObjectStore option in the externalClusters section
	Context("using minio as object storage", Ordered, func() {
		BeforeAll(func() {
			if !IsLocal() {
				Skip("This test is only executed on openshift and local")
			}
			const namespacePrefix = "recovery-barman-object-minio"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			By("creating ca and tls certificate secrets", func() {
				// create CA certificate
				_, caPair, err := testUtils.CreateSecretCA(namespace, clusterName, minioCaSecName, true, env)
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

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterSourceFileMinio, env)

			By("verify test connectivity to minio using barman-cloud-wal-archive script", func() {
				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (bool, error) {
					connectionStatus, err := testUtils.MinioTestConnectivityUsingBarmanCloudWalArchive(
						namespace, clusterName, primaryPod.GetName(), "minio", "minio123")
					if err != nil {
						return false, err
					}
					return connectionStatus, nil
				}, 60).Should(BeTrue())
			})
		})

		It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			externalClusterName, err := env.GetResourceNameFromYAML(externalClusterFileMinio)
			Expect(err).ToNot(HaveOccurred())

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)

			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceTakeFirstBackupFileMinio, false,
					testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(1),
					fmt.Sprintf("verify the number of backup %v is equals to 1", latestTar))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restoring cluster using a recovery barman object store, which is defined
			// in the externalClusters section
			AssertClusterRestore(namespace, externalClusterFileMinio, tableName, psqlClientPod)

			// verify test data on restored external cluster
			AssertDataExpectedCount(namespace, externalClusterName, tableName, 2, psqlClientPod)

			By("deleting the restored cluster", func() {
				err = DeleteResourcesFromFile(namespace, externalClusterFileMinio)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			externalClusterRestoreName := "restore-external-cluster-pitr"
			// We have already written 2 rows in test table 'to_restore' in above test now we will take current
			// timestamp. It will use to restore cluster from source using PITR

			By("getting currentTimestamp", func() {
				ts, err := testUtils.GetCurrentTimestamp(namespace, clusterName, env, psqlClientPod)
				*currentTimestamp = ts
				Expect(err).ToNot(HaveOccurred())
			})
			By(fmt.Sprintf("writing 2 more entries in table '%v'", tableName), func() {
				// insert 2 more rows entries 3,4 on the "app" database
				insertRecordIntoTable(namespace, clusterName, tableName, 3, psqlClientPod)
				insertRecordIntoTable(namespace, clusterName, tableName, 4, psqlClientPod)
			})
			By("creating second backup and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceTakeSecondBackupFileMinio, false,
					testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(2),
					fmt.Sprintf("verify the number of backup %v is equals to 2", latestTar))
			})
			var restoredCluster *apiv1.Cluster
			By("create a cluster from backup with PITR", func() {
				var err error
				restoredCluster, err = testUtils.CreateClusterFromExternalClusterBackupWithPITROnMinio(
					namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)
				Expect(err).NotTo(HaveOccurred())
			})
			AssertClusterWasRestoredWithPITRAndApplicationDB(
				namespace,
				externalClusterRestoreName,
				tableName,
				"00000002",
				psqlClientPod)
			By("delete restored cluster", func() {
				Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
			})
		})

		It("restore cluster from barman object using replica option in spec", func() {
			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, "for_restore_repl", psqlClientPod)

			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceTakeThirdBackupFileMinio, false,
					testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, latestTar)
				}, 60).Should(BeEquivalentTo(3),
					fmt.Sprintf("verify the number of backup %v is great than 3", latestTar))
			})

			// Replicating a cluster with asynchronous replication
			AssertClusterAsyncReplica(
				namespace,
				clusterSourceFileMinio,
				externalClusterFileMinioReplica,
				"for_restore_repl",
				psqlClientPod)
		})
	})

	Context("using azure blobs as object storage", func() {
		Context("storage account access authentication", Ordered, func() {
			BeforeAll(func() {
				if !IsAKS() {
					Skip("This test is only executed on AKS clusters")
				}
				azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
				azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
				const namespacePrefix = "recovery-barman-object-azure"
				var err error
				clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileAzure)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() error {
					return env.DeleteNamespace(namespace)
				})

				// The Azure Blob Storage should have been created ad-hoc for the test.
				// The credentials are retrieved from the environment variables, as we can't create
				// a fixture for them
				By("creating the Azure Blob Storage credentials", func() {
					AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds",
						azStorageAccount, azStorageKey)
				})

				// Create the cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzure, env)
			})

			It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				// Write a table and some data on the "app" database
				AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)
				AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// Create the backup
					testUtils.ExecuteBackup(namespace, sourceBackupFileAzure, false, testTimeouts[testUtils.BackupIsReady], env)
					AssertBackupConditionInClusterStatus(namespace, clusterName)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestore(namespace, externalClusterFileAzure, tableName, psqlClientPod)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				externalClusterName := "external-cluster-azure-pitr"

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					sourceBackupFileAzurePITR,
					azStorageAccount,
					azStorageKey,
					1,
					currentTimestamp,
					psqlClientPod)

				restoredCluster, err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzure(namespace,
					externalClusterName, clusterName, *currentTimestamp, "backup-storage-creds", azStorageAccount, env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterWasRestoredWithPITRAndApplicationDB(namespace, externalClusterName,
					tableName, "00000002", psqlClientPod)

				By("delete restored cluster", func() {
					Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
				})
			})
		})

		Context("storage account SAS Token authentication", Ordered, func() {
			BeforeAll(func() {
				if !IsAKS() {
					Skip("This test is only executed on AKS clusters")
				}
				azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
				azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
				const namespacePrefix = "cluster-backup-azure-blob-sas"
				var err error
				clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileAzureSAS)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() error {
					return env.DeleteNamespace(namespace)
				})

				// The Azure Blob Storage should have been created ad-hoc for the test,
				// we get the credentials from the environment variables as we can't create
				// a fixture for them
				By("creating the Azure Blob Container SAS Token credentials", func() {
					AssertCreateSASTokenCredentials(namespace, azStorageAccount, azStorageKey)
				})

				// Create the Cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzureSAS, env)
			})

			It("restores cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				// Write a table and some data on the "app" database
				AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)

				// Create a WAL on the primary and check if it arrives on
				// Azure Blob Storage within a short time
				AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// We create a Backup
					testUtils.ExecuteBackup(namespace, sourceBackupFileAzureSAS, false, testTimeouts[testUtils.BackupIsReady], env)
					AssertBackupConditionInClusterStatus(namespace, clusterName)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey,
							clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restore backup in a new cluster
				AssertClusterRestoreWithApplicationDB(namespace, clusterRestoreFileAzureSAS, tableName, psqlClientPod)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				externalClusterName := "external-cluster-azure-pitr"

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					sourceBackupFileAzurePITRSAS,
					azStorageAccount,
					azStorageKey,
					1,
					currentTimestamp,
					psqlClientPod)

				restoredCluster, err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzure(namespace,
					externalClusterName, clusterName, *currentTimestamp, "backup-storage-creds-sas", azStorageAccount, env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterWasRestoredWithPITRAndApplicationDB(namespace, externalClusterName,
					tableName, "00000002", psqlClientPod)

				By("delete restored cluster", func() {
					Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
				})
			})
		})
	})

	Context("using Azurite blobs as object storage", Ordered, func() {
		BeforeAll(func() {
			if IsAKS() {
				Skip("This test is not run on AKS")
			}
			const namespacePrefix = "recovery-barman-object-azurite"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			// Create and assert ca and tls certificate secrets on Azurite
			By("creating ca and tls certificate secrets", func() {
				err := testUtils.CreateCertificateSecretsOnAzurite(
					namespace,
					clusterName,
					azuriteCaSecName,
					azuriteTLSSecName,
					env)
				Expect(err).ToNot(HaveOccurred())
			})
			// Setup Azurite and az cli along with PostgreSQL cluster
			prepareClusterBackupOnAzurite(
				namespace,
				clusterName,
				azuriteBlobSampleFile,
				backupFileAzurite,
				tableName,
				psqlClientPod)
		})

		It("restore cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			// Restore backup in a new cluster
			AssertClusterRestoreWithApplicationDB(namespace, externalClusterFileAzurite, tableName, psqlClientPod)
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			const (
				externalClusterRestoreName = "restore-external-cluster-pitr"
				backupFileAzuritePITR      = fixturesBackupDir + "backup-azurite-pitr.yaml"
			)

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFileAzuritePITR, currentTimestamp, psqlClientPod)

			//  Create a cluster from a particular time using external backup.
			restoredCluster, err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzurite(
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterWasRestoredWithPITRAndApplicationDB(
				namespace,
				externalClusterRestoreName,
				tableName,
				"00000002",
				psqlClientPod)

			By("delete restored cluster", func() {
				Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
			})
		})
	})
})

var _ = Describe("Backup and restore Safety", Label(tests.LabelBackupRestore), func() {
	const (
		level = tests.High

		clusterSampleFile = fixturesDir + "/backup/backup_restore_safety/cluster-with-backup-minio.yaml.template"
	)

	var namespace, clusterName, namespace2 string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+namespace+CurrentSpecReport().LeafNodeText+".log")
			env.DumpNamespaceObjects(namespace2, "out/"+namespace2+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	Context("using minio as object storage", Ordered, func() {
		// This is a set of tests using a minio server to ensure backup and safet
		// in case user configures the same destination path for more backups

		const (
			clusterRestoreSampleFile  = fixturesDir + "/backup/backup_restore_safety/external-clusters-minio.yaml.template"
			clusterRestoreSampleFile2 = fixturesDir + "/backup/backup_restore_safety/external-clusters-minio-2.yaml.template"
			clusterRestoreSampleFile3 = fixturesDir + "/backup/backup_restore_safety/external-clusters-minio-3.yaml.template"
			clusterRestoreSampleFile4 = fixturesDir + "/backup/backup_restore_safety/external-clusters-minio-4.yaml.template"
			sourceBackup              = fixturesDir + "/backup/backup_restore_safety/backup-source-cluster.yaml"
			restoreBackup             = fixturesDir + "/backup/backup_restore_safety/backup-cluster-2.yaml"
		)
		BeforeAll(func() {
			if !IsLocal() {
				Skip("This test is only run on local cluster")
			}
			// This name is used in yaml file, keep it as const
			namespace = "backup-safety-1"
			namespacePrefix2 := "backup-safety-2"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(clusterSampleFile)
			Expect(err).ToNot(HaveOccurred())

			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			namespace2, err = env.CreateUniqueNamespace(namespacePrefix2)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace2)
			})

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})

			// setting up default minio
			By("setting up minio", func() {
				minio, err := testUtils.MinioDefaultSetup(namespace)
				Expect(err).ToNot(HaveOccurred())

				err = testUtils.InstallMinio(env, minio, uint(testTimeouts[testUtils.MinioInstallation]))
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				minioClient := testUtils.MinioDefaultClient(namespace)
				err := testUtils.PodCreateAndWaitForReady(env, &minioClient, 240)
				Expect(err).ToNot(HaveOccurred())
			})

			// Creates the cluster
			AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

			// Taking backup of source cluster
			testUtils.ExecuteBackup(namespace, sourceBackup, false, testTimeouts[testUtils.BackupIsReady], env)
		})

		It("restore a cluster with different backup destination and creates another cluster with same path as "+
			"source cluster and it fails", func() {
			restoredClusterName, err := env.GetResourceNameFromYAML(clusterRestoreSampleFile2)
			Expect(err).ToNot(HaveOccurred())

			// Deleting  source cluster since we have backup to restore.
			By("deleting the original cluster", func() {
				err = DeleteResourcesFromFile(namespace, clusterSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})

			// Restoring cluster form source backup
			AssertCreateCluster(namespace, restoredClusterName, clusterRestoreSampleFile2, env)

			// Taking backup of restore cluster which will be used to create another cluster further
			By("taking backup of the restore cluster", func() {
				testUtils.ExecuteBackup(namespace, restoreBackup, false, testTimeouts[testUtils.BackupIsReady], env)
			})

			// Restoring cluster from second backup
			By("restoring the cluster from the second backup", func() {
				err = CreateResourcesFromFileWithError(namespace, clusterRestoreSampleFile3)
				Expect(err).ShouldNot(HaveOccurred())
			})

			// Verifying the cluster creation errors since it will fail
			// it must have the error log message for barman cloud.
			Eventually(func() (int, error) {
				podList := &corev1.PodList{}
				err := testUtils.GetObjectList(env, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{"job-name": "pg-backup-minio-1-full-recovery"})
				if err != nil {
					return -1, err
				}
				return len(podList.Items), nil
			}, 60).Should(BeNumerically(">", 1))

			podList := &corev1.PodList{}
			err = testUtils.GetObjectList(env, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"job-name": "pg-backup-minio-1-full-recovery"})
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range podList.Items {
				Eventually(func() bool {
					podLogs, _ := env.GetPodLogs(namespace, pod.GetName())
					return strings.Contains(podLogs,
						"ERROR: WAL archive check failed for server "+
							"pg-backup-minio: Expected empty archive")
				}, 60).Should(BeTrue())

				break
			}
		})

		It("restore a cluster with different backup destination and creates another cluster with same "+
			"backup destination as restored cluster and it fails", func() {
			err := DeleteResourcesFromFile(namespace, clusterRestoreSampleFile3)
			Expect(err).ToNot(HaveOccurred())

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace2, "backup-storage-creds", "minio", "minio123")
			})

			err = CreateResourcesFromFileWithError(namespace2, clusterRestoreSampleFile4)
			Expect(err).ShouldNot(HaveOccurred())

			// Verifying the cluster creation errors since it will fail
			// it must have the error log message for barman cloud.
			Eventually(func() (int, error) {
				podList := &corev1.PodList{}
				err := testUtils.GetObjectList(env, podList, ctrlclient.InNamespace(namespace2),
					ctrlclient.MatchingLabels{"job-name": "external-cluster-minio-1-1-full-recovery"})
				if err != nil {
					return -1, err
				}
				return len(podList.Items), nil
			}, 60).Should(BeNumerically(">", 1))

			podList := &corev1.PodList{}
			err = testUtils.GetObjectList(env, podList, ctrlclient.InNamespace(namespace2),
				ctrlclient.MatchingLabels{"job-name": "external-cluster-minio-1-1-full-recovery"})
			Expect(err).ToNot(HaveOccurred())

			isLogContainsFailure := false
			for _, pod := range podList.Items {
				podLogs, _ := env.GetPodLogs(namespace2, pod.GetName())
				if strings.Contains(podLogs,
					"ERROR: WAL archive check failed for server "+
						"external-cluster-minio-1: Expected empty archive") {
					isLogContainsFailure = true
					break
				}
			}
			Expect(isLogContainsFailure).Should(BeTrue())
		})

		It("creates a cluster with backup and "+
			"also creates a cluster with same backup location and it fails", func() {
			By("create cluster in second namespace with same backup location", func() {
				AssertCreateCluster(namespace2, clusterName, clusterSampleFile, env)
			})

			// Verifying the cluster creation errors since it will fail
			// it must have the error log message for barman cloud.
			By("verify status.conditions contains error", func() {
				// fetching cluster condition
				Eventually(func() (bool, error) {
					clusterCondition, err := testUtils.GetConditionsInClusterStatus(namespace2,
						"pg-backup-minio", env, apiv1.ConditionContinuousArchiving)
					if err != nil {
						return false, err
					}
					if clusterCondition.Message != "" {
						return strings.Contains(clusterCondition.Message,
							"unexpected failure invoking barman-cloud-wal-archive"), nil
					}
					return false, nil
				}, 60).Should(BeTrue())
			})
		})
	})
})
