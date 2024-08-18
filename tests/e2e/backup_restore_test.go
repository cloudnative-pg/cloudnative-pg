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
	"path/filepath"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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

		barmanCloudBackupLogEntry = "Starting barman-cloud-backup"
	)

	currentTimestamp := new(string)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("using minio as object storage for backup", Ordered, func() {
		// This is a set of tests using a minio server deployed in the same
		// namespace as the cluster. Since each cluster is installed in its
		// own namespace, they can share the configuration file
		var namespace, clusterName string
		const (
			backupFile              = fixturesDir + "/backup/minio/backup-minio.yaml"
			customQueriesSampleFile = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
		)

		clusterWithMinioSampleFile := fixturesDir + "/backup/minio/cluster-with-backup-minio.yaml.template"

		BeforeAll(func() {
			if !IsLocal() {
				Skip("This test is only run on local clusters")
			}
			const namespacePrefix = "cluster-backup-minio"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
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
						namespace, clusterName, primaryPod.GetName(), "minio", "minio123", minioEnv.ServiceName)
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
			var backup *apiv1.Backup
			restoredClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			backupName, err := env.GetResourceNameFromYAML(backupFile)
			Expect(err).ToNot(HaveOccurred())
			// Create required test data
			AssertCreationOfTestDataForTargetDB(env, namespace, clusterName, targetDBOne, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, clusterName, targetDBTwo, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, clusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)
			latestTar := minioPath(clusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster and verifying it exists on minio, backup path is %v", latestTar), func() {
				backup = testUtils.ExecuteBackup(namespace, backupFile, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.LastSuccessfulBackup, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.LastFailedBackup, err
				}, 30).Should(BeEmpty())
			})

			By("verifying the backup is using the expected barman-cloud-backup options", func() {
				Expect(backup).ToNot(BeNil())
				Expect(backup.Status.InstanceID).ToNot(BeNil())
				logEntries, err := testUtils.ParseJSONLogs(namespace, backup.Status.InstanceID.PodName, env)
				Expect(err).ToNot(HaveOccurred())
				expectedBaseBackupOptions := []string{
					"--immediate-checkpoint",
					"--min-chunk-size=5MB",
					"--read-timeout=59",
				}
				result, err := testUtils.CheckOptionsForBarmanCommand(
					logEntries,
					barmanCloudBackupLogEntry,
					backup.Name,
					backup.Status.InstanceID.PodName,
					expectedBaseBackupOptions,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(BeTrue())
			})

			By("executing a second backup and verifying the number of backups on minio", func() {
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
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
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
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
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)

			cluster, err := env.GetCluster(namespace, restoredClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertMetricsData(namespace, targetDBOne, targetDBTwo, targetDBSecret, cluster)

			previous := 0
			latestGZ := filepath.Join("*", clusterName, "*", "*.history.gz")
			By(fmt.Sprintf("checking the previous number of .history files in minio, history file name is %v",
				latestGZ), func() {
				previous, err = testUtils.CountFilesOnMinio(minioEnv, latestGZ)
				Expect(err).ToNot(HaveOccurred())
			})

			AssertSwitchover(namespace, clusterName, env)

			By("checking the number of .history after switchover", func() {
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestGZ)
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
			AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBOne, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBTwo, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, targetClusterName, tableName)

			AssertArchiveWalOnMinio(namespace, targetClusterName, targetClusterName)
			latestTar := minioPath(targetClusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster from standby and verifying it exists on minio, backup path is %v",
				latestTar), func() {
				testUtils.ExecuteBackup(namespace, backupStandbyFile, true, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, targetClusterName)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
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
			AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBOne, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBTwo, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, targetClusterName, tableName)

			AssertArchiveWalOnMinio(namespace, targetClusterName, targetClusterName)
			latestTar := minioPath(targetClusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster from standby (defined in backup file) and verifying it exists on minio,"+
				" backup path is %v", latestTar), func() {
				testUtils.ExecuteBackup(namespace, backupWithTargetFile, true, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, targetClusterName)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
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

			// Create the cluster with custom serverName in the backup spec
			AssertCreateCluster(namespace, customClusterName, clusterWithMinioCustomSampleFile, env)

			// Create required test data
			AssertCreationOfTestDataForTargetDB(env, namespace, customClusterName, targetDBOne, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, customClusterName, targetDBTwo, testTableName)
			AssertCreationOfTestDataForTargetDB(env, namespace, customClusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, customClusterName, tableName)

			AssertArchiveWalOnMinio(namespace, customClusterName, clusterServerName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, backupFileCustom, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, customClusterName)
				latestBaseTar := minioPath(clusterServerName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestBaseTar)
				}, 60).Should(BeEquivalentTo(1),
					fmt.Sprintf("verify the number of backup %v is equals to 1", latestBaseTar))
				// this is the second backup we take on the bucket
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, customClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)

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
				return testUtils.CountFilesOnMinio(minioEnv, latestBaseTar)
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
			)

			cluster, err := testUtils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFilePITR,
				*currentTimestamp,
				env,
			)
			Expect(err).NotTo(HaveOccurred())
			AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testUtils.ClusterIsReady], env)

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000003")

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
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
				}, 60).Should(BeNumerically(">=", 2),
					fmt.Sprintf("verify the number of backup %v is great than 2", latestTar))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})

		It("verify tags in backed files", func() {
			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)
			tags, err := testUtils.GetFileTagsOnMinio(minioEnv, minioPath(clusterName, "*1.gz"))
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

			tags, err = testUtils.GetFileTagsOnMinio(minioEnv, minioPath(clusterName, "*.history.gz"))
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
		var namespace, clusterName string

		BeforeAll(func() {
			if !IsAKS() {
				Skip("This test is only run on AKS clusters")
			}
			const namespacePrefix = "cluster-backup-azure-blob"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(azureBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// The Azure Blob Storage should have been created ad-hoc for the test.
			// The credentials are retrieved from the environment variables, as we can't create
			// a fixture for them
			By("creating the Azure Blob Storage credentials", func() {
				AssertStorageCredentialsAreCreated(
					namespace,
					"backup-storage-creds",
					env.AzureConfiguration.StorageAccount,
					env.AzureConfiguration.StorageKey,
				)
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, azureBlobSampleFile, env)
		})

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, clusterName, tableName)
			AssertArchiveWalOnAzureBlob(namespace, clusterName, env.AzureConfiguration)
			By("uploading a backup", func() {
				// We create a backup
				testUtils.ExecuteBackup(namespace, backupFile, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnAzureBlobStorage(env.AzureConfiguration, clusterName, "data.tar")
				}, 30).Should(BeNumerically(">=", 1))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)

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
				return testUtils.CountFilesOnAzureBlobStorage(env.AzureConfiguration,
					clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 2))
		})

		It("backs up and restore a cluster with PITR", func() {
			restoredClusterName := "restore-cluster-azure-pitr"

			prepareClusterForPITROnAzureBlob(
				namespace,
				clusterName,
				backupFile,
				env.AzureConfiguration,
				2,
				currentTimestamp,
			)

			AssertArchiveWalOnAzureBlob(namespace, clusterName, env.AzureConfiguration)

			cluster, err := testUtils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFile,
				*currentTimestamp,
				env,
			)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testUtils.ClusterIsReady], env)

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000002")
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
					return testUtils.CountFilesOnAzureBlobStorage(env.AzureConfiguration,
						clusterName, "data.tar")
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
		var namespace, clusterName string

		BeforeAll(func() {
			if !(IsLocal() || IsGKE() || IsOpenshift()) {
				Skip("This test is only executed on gke, openshift and local")
			}
			const namespacePrefix = "cluster-backup-azurite"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// Create and assert ca and tls certificate secrets on Azurite
			By("creating ca and tls certificate secrets", func() {
				err := testUtils.CreateCertificateSecretsOnAzurite(namespace, clusterName,
					azuriteCaSecName, azuriteTLSSecName, env)
				Expect(err).ToNot(HaveOccurred())
			})
			// Setup Azurite and az cli along with Postgresql cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFile, tableName)
		})

		It("restores a backed up cluster", func() {
			// Restore backup in a new cluster
			AssertClusterRestoreWithApplicationDB(namespace, clusterRestoreSampleFile, tableName)
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

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFilePITR, currentTimestamp)

			cluster, err := testUtils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFilePITR,
				*currentTimestamp,
				env,
			)
			Expect(err).NotTo(HaveOccurred())
			AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testUtils.ClusterIsReady], env)

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000002")

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

	currentTimestamp := new(string)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// Restore cluster using a recovery object store, that is a backup of another cluster,
	// created by Barman Cloud, and defined via the barmanObjectStore option in the externalClusters section
	Context("using minio as object storage", Ordered, func() {
		var namespace, clusterName string

		BeforeAll(func() {
			if !IsLocal() {
				Skip("This test is only executed on local")
			}
			const namespacePrefix = "recovery-barman-object-minio"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterSourceFileMinio, env)

			By("verify test connectivity to minio using barman-cloud-wal-archive script", func() {
				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (bool, error) {
					connectionStatus, err := testUtils.MinioTestConnectivityUsingBarmanCloudWalArchive(
						namespace, clusterName, primaryPod.GetName(), "minio", "minio123", minioEnv.ServiceName)
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
			AssertCreateTestData(env, namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceTakeFirstBackupFileMinio, false,
					testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)

				// TODO: this is to force a CHECKPOINT when we run the backup on standby.
				// This should be better handled inside ExecuteBackup
				AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(1),
					fmt.Sprintf("verify the number of backup %v is equals to 1", latestTar))
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restoring cluster using a recovery barman object store, which is defined
			// in the externalClusters section
			AssertClusterRestore(namespace, externalClusterFileMinio, tableName)

			// verify test data on restored external cluster
			AssertDataExpectedCount(env, namespace, externalClusterName, tableName, 2)

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
				ts, err := testUtils.GetCurrentTimestamp(namespace, clusterName, env)
				*currentTimestamp = ts
				Expect(err).ToNot(HaveOccurred())
			})
			By(fmt.Sprintf("writing 2 more entries in table '%v'", tableName), func() {
				forward, conn, err := testUtils.ForwardPSQLConnection(
					env,
					namespace,
					clusterName,
					testUtils.AppDBName,
					apiv1.ApplicationUserSecretSuffix,
				)
				defer func() {
					_ = conn.Close()
					forward.Stop()
				}()
				Expect(err).ToNot(HaveOccurred())
				// insert 2 more rows entries 3,4 on the "app" database
				insertRecordIntoTable(tableName, 3, conn)
				insertRecordIntoTable(tableName, 4, conn)
			})
			By("creating second backup and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceTakeSecondBackupFileMinio, false,
					testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
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
			)
			By("delete restored cluster", func() {
				Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
			})
		})

		It("restore cluster from barman object using replica option in spec", func() {
			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, clusterName, "for_restore_repl")

			AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceTakeThirdBackupFileMinio, false,
					testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
				latestTar := minioPath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(3),
					fmt.Sprintf("verify the number of backup %v is great than 3", latestTar))
			})

			// Replicating a cluster with asynchronous replication
			AssertClusterAsyncReplica(
				namespace,
				clusterSourceFileMinio,
				externalClusterFileMinioReplica,
				"for_restore_repl",
			)
		})
	})

	Context("using azure blobs as object storage", func() {
		Context("storage account access authentication", Ordered, func() {
			var namespace, clusterName string
			BeforeAll(func() {
				if !IsAKS() {
					Skip("This test is only executed on AKS clusters")
				}
				const namespacePrefix = "recovery-barman-object-azure"
				var err error
				clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileAzure)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// The Azure Blob Storage should have been created ad-hoc for the test.
				// The credentials are retrieved from the environment variables, as we can't create
				// a fixture for them
				By("creating the Azure Blob Storage credentials", func() {
					AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds",
						env.AzureConfiguration.StorageAccount, env.AzureConfiguration.StorageKey)
				})

				// Create the cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzure, env)
			})

			It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				// Write a table and some data on the "app" database
				AssertCreateTestData(env, namespace, clusterName, tableName)
				AssertArchiveWalOnAzureBlob(namespace, clusterName, env.AzureConfiguration)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// Create the backup
					testUtils.ExecuteBackup(namespace, sourceBackupFileAzure, false, testTimeouts[testUtils.BackupIsReady], env)
					AssertBackupConditionInClusterStatus(namespace, clusterName)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return testUtils.CountFilesOnAzureBlobStorage(env.AzureConfiguration, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestore(namespace, externalClusterFileAzure, tableName)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				externalClusterName := "external-cluster-azure-pitr"

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					sourceBackupFileAzurePITR,
					env.AzureConfiguration,
					1,
					currentTimestamp,
				)

				restoredCluster, err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzure(
					namespace,
					externalClusterName,
					clusterName,
					*currentTimestamp,
					"backup-storage-creds",
					env.AzureConfiguration.StorageAccount,
					env.AzureConfiguration.BlobContainer,
					env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterWasRestoredWithPITRAndApplicationDB(
					namespace,
					externalClusterName,
					tableName,
					"00000002",
				)

				By("delete restored cluster", func() {
					Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
				})
			})
		})

		Context("storage account SAS Token authentication", Ordered, func() {
			var namespace, clusterName string
			BeforeAll(func() {
				if !IsAKS() {
					Skip("This test is only executed on AKS clusters")
				}
				const namespacePrefix = "cluster-backup-azure-blob-sas"
				var err error
				clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileAzureSAS)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// The Azure Blob Storage should have been created ad-hoc for the test,
				// we get the credentials from the environment variables as we can't create
				// a fixture for them
				By("creating the Azure Blob Container SAS Token credentials", func() {
					AssertCreateSASTokenCredentials(namespace, env.AzureConfiguration.StorageAccount,
						env.AzureConfiguration.StorageKey)
				})

				// Create the Cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzureSAS, env)
			})

			It("restores cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				// Write a table and some data on the "app" database
				AssertCreateTestData(env, namespace, clusterName, tableName)

				// Create a WAL on the primary and check if it arrives in the
				// Azure Blob Storage within a short time
				AssertArchiveWalOnAzureBlob(namespace, clusterName, env.AzureConfiguration)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// We create a Backup
					testUtils.ExecuteBackup(namespace, sourceBackupFileAzureSAS, false, testTimeouts[testUtils.BackupIsReady], env)
					AssertBackupConditionInClusterStatus(namespace, clusterName)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return testUtils.CountFilesOnAzureBlobStorage(env.AzureConfiguration, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restore backup in a new cluster
				AssertClusterRestoreWithApplicationDB(namespace, clusterRestoreFileAzureSAS, tableName)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				externalClusterName := "external-cluster-azure-pitr"

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					sourceBackupFileAzurePITRSAS,
					env.AzureConfiguration,
					1,
					currentTimestamp,
				)

				restoredCluster, err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzure(
					namespace,
					externalClusterName,
					clusterName,
					*currentTimestamp,
					"backup-storage-creds-sas",
					env.AzureConfiguration.StorageAccount,
					env.AzureConfiguration.BlobContainer,
					env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterWasRestoredWithPITRAndApplicationDB(
					namespace,
					externalClusterName,
					tableName,
					"00000002",
				)

				By("delete restored cluster", func() {
					Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
				})
			})
		})
	})

	Context("using Azurite blobs as object storage", Ordered, func() {
		var namespace, clusterName string
		BeforeAll(func() {
			if IsAKS() {
				Skip("This test is not run on AKS")
			}
			const namespacePrefix = "recovery-barman-object-azurite"
			var err error
			clusterName, err = env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

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
			)
		})

		It("restore cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			// Restore backup in a new cluster
			AssertClusterRestoreWithApplicationDB(namespace, externalClusterFileAzurite, tableName)
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			const (
				externalClusterRestoreName = "restore-external-cluster-pitr"
				backupFileAzuritePITR      = fixturesBackupDir + "backup-azurite-pitr.yaml"
			)

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFileAzuritePITR, currentTimestamp)

			//  Create a cluster from a particular time using external backup.
			restoredCluster, err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzurite(
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterWasRestoredWithPITRAndApplicationDB(
				namespace,
				externalClusterRestoreName,
				tableName,
				"00000002",
			)

			By("delete restored cluster", func() {
				Expect(testUtils.DeleteObject(env, restoredCluster)).To(Succeed())
			})
		})
	})
})
