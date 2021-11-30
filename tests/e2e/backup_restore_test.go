/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"os"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup and restore", func() {
	const (
		level = tests.High

		azuriteBlobSampleFile = fixturesDir + "/backup/azurite/cluster-backup.yaml"

		tableName = "to_restore"
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
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("using minio as object storage", func() {
		// This is a set of tests using a minio server deployed in the same
		// namespace as the cluster. Since each cluster is installed in its
		// own namespace, they can share the configuration file

		const (
			clusterWithMinioSampleFile = fixturesDir + "/backup/minio/cluster-with-backup-minio.yaml"
			backupFile                 = fixturesDir + "/backup/minio/backup-minio.yaml"
		)

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			const (
				targetDBOne              = "test"
				targetDBTwo              = "test1"
				targetDBSecret           = "secret_test"
				testTableName            = "test_table"
				customQueriesSampleFile  = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
				clusterRestoreSampleFile = fixturesDir + "/backup/cluster-from-restore.yaml"
			)
			namespace = "cluster-backup-minio"

			clusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			restoredClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})

			By("setting up minio", func() {
				InstallMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				InstallMinioClient(namespace)
			})

			// Create ConfigMap and secrets to verify metrics for target database after backup restore
			AssertCustomMetricsResourcesExist(namespace, customQueriesSampleFile, 1, 1)

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterWithMinioSampleFile, env)

			// Create required test data
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBOne, testTableName)
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBTwo, testTableName)
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				utils.ExecuteBackup(namespace, backupFile, env)

				Eventually(func() (int, error) {
					return CountFilesOnMinio(namespace, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster := &apiv1.Cluster{}
					err := env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
						cluster)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)

			AssertMetricsData(namespace, restoredClusterName, targetDBOne, targetDBTwo, targetDBSecret)

			previous := 0

			By("checking the previous number of .history files in minio", func() {
				previous, err = CountFilesOnMinio(namespace, "*.history.gz")
				Expect(err).ToNot(HaveOccurred())
			})

			AssertSwitchOver(namespace, clusterName, env)

			By("checking the number of .history after switchover", func() {
				Eventually(func() (int, error) {
					return CountFilesOnMinio(namespace, "*.history.gz")
				}, 60).Should(BeNumerically(">", previous))
			})
		})

		// We create a cluster and a scheduled backup, then it is patched to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			namespace = "scheduled-backups-suspend-minio"
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-minio.yaml"
			clusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})

			By("setting up minio", func() {
				InstallMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				InstallMinioClient(namespace)
			})

			AssertCreateCluster(namespace, clusterName, clusterWithMinioSampleFile, env)

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				Eventually(func() (int, error) {
					return CountFilesOnMinio(namespace, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})

		// Create a scheduled backup with the 'immediate' option enable. We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups 'immediate' option", func() {
			namespace = "scheduled-backups-immediate-minio"
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-minio.yaml"
			clusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})

			By("setting up minio", func() {
				InstallMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				InstallMinioClient(namespace)
			})

			AssertCreateCluster(namespace, clusterName, clusterWithMinioSampleFile, env)

			AssertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return CountFilesOnMinio(namespace, "data.tar")
			}, 30).Should(BeNumerically("==", 1))
		})

		It("backs up and restore a cluster with PITR", func() {
			namespace = "backup-restore-pitr-minio"
			clusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			restoredClusterName := "restore-cluster-pitr"

			prepareClusterForPITROnMinio(
				namespace, clusterName, clusterWithMinioSampleFile, backupFile, tableName, currentTimestamp)

			err = utils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFile,
				*currentTimestamp,
				env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, restoredClusterName, tableName)
		})
	})

	Context("using azure blobs as object storage with storage account access authentication", func() {
		// We must be careful here. All the clusters use the same remote storage
		// and that means that we must use different cluster names otherwise
		// we risk mixing WALs and backups

		const azureContextLevel = tests.Medium
		BeforeEach(func() {
			if testLevelEnv.Depth < int(azureContextLevel) {
				Skip("Test depth is lower than the amount requested for this test")
			}

			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if !isAKS {
				Skip("This test is only executed on AKS clusters")
			}

			azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
			azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
		})

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			namespace = "cluster-backup-azure-blob"
			const azureBlobSampleFile = fixturesDir + "/backup/azure_blob/cluster-with-backup-azure-blob.yaml"
			const clusterRestoreSampleFile = fixturesDir + "/backup/cluster-from-restore.yaml"
			clusterName, err := env.GetResourceNameFromYAML(azureBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// The Azure Blob Storage should have been created ad-hoc for the test.
			// The credentials are retrieved from the environment variables, as we can't create
			// a fixture for them
			By("creating the Azure Blob Storage credentials", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", azStorageAccount, azStorageKey)
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, azureBlobSampleFile, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)
			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)
			By("uploading a backup", func() {
				// We create a backup
				backupFile := fixturesDir + "/backup/azure_blob/backup-azure-blob.yaml"
				utils.ExecuteBackup(namespace, backupFile, env)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return utils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
				}, 30).Should(BeNumerically(">=", 1))
				Eventually(func() (string, error) {
					cluster := &apiv1.Cluster{}
					err := env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
						cluster)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)
		})

		// We create a cluster, create a scheduled backup, patch it to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			namespace = "scheduled-backups-suspend-azure-blob"
			const sampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/cluster-with-backup-azure-blob.yaml"
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-azure-blob.yaml"
			clusterName, err := env.GetResourceNameFromYAML(sampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating the Azure Blob Storage credentials", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", azStorageAccount, azStorageKey)
			})

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 480)

				// AssertScheduledBackupsImmediate creates at least two backups, we should find
				// their base backups
				Eventually(func() (int, error) {
					return utils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})
			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})

		// Create a scheduled backup with the 'immediate' option enabled. We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups 'immediate' option", func() {
			namespace = "scheduled-backup-immediate-azure-blob"
			const sampleFile = fixturesDir + "/backup/scheduled_backup_immediate/cluster-with-backup-azure-blob.yaml"
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-azure-blob.yaml"
			clusterName, err := env.GetResourceNameFromYAML(sampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating the Azure Blob Storage credentials", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", azStorageAccount, azStorageKey)
			})

			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

			// Only one data.tar files should be present
			Eventually(func() (int, error) {
				return utils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 1))
		})

		It("backs up and restore a cluster with PITR", func() {
			namespace = "backup-azure-blob-pitr"

			const (
				azureBlobSampleFile = fixturesDir + "/backup/pitr/cluster-with-backup-azure-blob.yaml"
				backupFile          = fixturesDir + "/backup/pitr/backup-azure-blob.yaml"
			)

			clusterName, err := env.GetResourceNameFromYAML(azureBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			restoredClusterName := "restore-cluster-azure-pitr"

			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			prepareClusterForPITROnAzureBlob(
				namespace,
				clusterName,
				azureBlobSampleFile,
				backupFile,
				azStorageAccount,
				azStorageKey,
				tableName,
				currentTimestamp)

			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

			err = utils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFile,
				*currentTimestamp,
				env)
			Expect(err).ToNot(HaveOccurred())

			// Restore backup in a new cluster
			AssertClusterRestorePITR(namespace, restoredClusterName, tableName)
		})
	})

	Context("using Azurite blobs as object storage", func() {
		// This is a set of tests using an Azurite server deployed in the same
		// namespace as the cluster. Since each cluster is installed in its
		// own namespace, they can share the configuration file

		BeforeEach(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if isAKS {
				Skip("This test is only executed on gke, openshift and local")
			}
		})

		const (
			clusterRestoreSampleFile  = fixturesDir + "/backup/azurite/cluster-from-restore.yaml"
			scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-azurite.yaml"
			scheduledBackupImmediateSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-azurite.yaml"
			backupFile = fixturesDir + "/backup/azurite/backup.yaml"
		)

		It("restores a backed up cluster", func() {
			namespace = "cluster-backup-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Setup Azurite and az cli along with Postgresql cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFile, tableName)

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)
		})

		// We create a cluster, create a scheduled backup, patch it to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			namespace = "scheduled-backups-suspend-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			prepareClusterOnAzurite(namespace, clusterName, azuriteBlobSampleFile)

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				Eventually(func() (int, error) {
					return utils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})

		// Create a scheduled backup with the 'immediate' option enabled.
		// We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups immediate option", func() {
			namespace = "scheduled-backups-immediate-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupImmediateSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Restore backup in a new cluster
			prepareClusterOnAzurite(namespace, clusterName, azuriteBlobSampleFile)

			AssertScheduledBackupsImmediate(namespace, scheduledBackupImmediateSampleFile, scheduledBackupName)

			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return utils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 1))
		})

		It("backs up and restore a cluster with PITR", func() {
			namespace = "backup-restore-pitr-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			restoredClusterName := "restore-cluster-pitr"

			prepareClusterForPITROnAzurite(
				namespace,
				clusterName,
				azuriteBlobSampleFile,
				backupFile,
				tableName,
				currentTimestamp)

			err = utils.CreateClusterFromBackupUsingPITR(
				namespace,
				restoredClusterName,
				backupFile,
				*currentTimestamp,
				env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, restoredClusterName, tableName)
		})
	})
})

var _ = Describe("Clusters Recovery From Barman Object Store", func() {
	const (
		level = tests.High

		fixturesBackupDir               = fixturesDir + "/backup/recovery_external_clusters/"
		azuriteBlobSampleFile           = fixturesDir + "/backup/azurite/cluster-backup.yaml"
		backupFileAzurite               = fixturesBackupDir + "backup-azurite-02.yaml"
		clusterRestoreFileAzureSAS      = fixturesBackupDir + "cluster-from-restore-sas.yaml"
		clusterSourceFileAzure          = fixturesBackupDir + "source-cluster-azure-blob-01.yaml"
		clusterSourceFileAzurePITR      = fixturesBackupDir + "source-cluster-azure-blob-pitr.yaml"
		clusterSourceFileAzurePITRSAS   = fixturesBackupDir + "source-cluster-azure-blob-pitr-sas.yaml"
		clusterSourceFileAzureSAS       = fixturesBackupDir + "cluster-with-backup-azure-blob-sas.yaml"
		clusterSourceFileMinio          = fixturesBackupDir + "source-cluster-minio-01.yaml"
		externalClusterFileAzure        = fixturesBackupDir + "external-clusters-azure-blob-03.yaml"
		externalClusterFileAzurite      = fixturesBackupDir + "external-clusters-azurite-03.yaml"
		externalClusterFileMinio        = fixturesBackupDir + "external-clusters-minio-03.yaml"
		externalClusterFileMinioReplica = fixturesBackupDir + "external-clusters-minio-replica-04.yaml"
		sourceBackupFileAzure           = fixturesBackupDir + "backup-azure-blob-02.yaml"
		sourceBackupFileAzurePITR       = fixturesBackupDir + "backup-azure-blob-pitr.yaml"
		sourceBackupFileAzurePITRSAS    = fixturesBackupDir + "backup-azure-blob-pitr-sas.yaml"
		sourceBackupFileAzureSAS        = fixturesBackupDir + "backup-azure-blob-sas.yaml"
		sourceBackupFileMinio           = fixturesBackupDir + "backup-minio-02.yaml"

		tableName = "to_restore"
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
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	// Restore cluster using a recovery object store, that is a backup of another cluster,
	// created by Barman Cloud, and defined via the barmanObjectStore option in the externalClusters section
	Context("using minio as object storage", func() {
		It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-minio"
			clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())

			externalClusterName, err := env.GetResourceNameFromYAML(externalClusterFileMinio)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})
			By("setting up minio", func() {
				InstallMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				InstallMinioClient(namespace)
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterSourceFileMinio, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				utils.ExecuteBackup(namespace, sourceBackupFileMinio, env)
				Eventually(func() (int, error) {
					return CountFilesOnMinio(namespace, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster := &apiv1.Cluster{}
					err := env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
						cluster)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restoring cluster using a recovery barman object store, which is defined
			// in the externalClusters section
			AssertClusterRestore(namespace, externalClusterFileMinio, tableName)

			// verify test data on restored external cluster
			primaryPodInfo, err := env.GetClusterPrimary(namespace, externalClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertDataExpectedCount(namespace, primaryPodInfo.GetName(), tableName, 2)
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-pitr-minio"
			clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())
			externalClusterRestoreName := "restore-external-cluster-pitr"

			prepareClusterForPITROnMinio(
				namespace, clusterName, clusterSourceFileMinio, sourceBackupFileMinio, tableName, currentTimestamp)

			err = utils.CreateClusterFromExternalClusterBackupWithPITROnMinio(
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)

			Expect(err).NotTo(HaveOccurred())
			AssertClusterRestorePITR(namespace, externalClusterRestoreName, tableName)
		})
		It("restore cluster from barman object using replica option in spec", func() {
			namespace = "recovery-barman-object-replica-minio"
			clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})
			By("setting up minio", func() {
				InstallMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly.
			By("setting up minio client pod", func() {
				InstallMinioClient(namespace)
			})

			// Create the Cluster
			AssertCreateCluster(namespace, clusterName, clusterSourceFileMinio, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				utils.ExecuteBackup(namespace, sourceBackupFileMinio, env)
				Eventually(func() (int, error) {
					return CountFilesOnMinio(namespace, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
			})

			// Replicating a cluster with asynchronous replication
			AssertClusterAsyncReplica(namespace, clusterSourceFileMinio, externalClusterFileMinioReplica, tableName)
		})
	})

	Context("using azure blobs as object storage", func() {
		const azureContextLevel = tests.Medium
		BeforeEach(func() {
			if testLevelEnv.Depth < int(azureContextLevel) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if !isAKS {
				Skip("This test is only executed on AKS clusters")
			}
			azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
			azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
		})
		Context("storage account access authentication", func() {
			It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				namespace = "recovery-barman-object-azure"
				clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileAzure)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())

				// The Azure Blob Storage should have been created ad-hoc for the test.
				// The credentials are retrieved from the environment variables, as we can't create
				// a fixture for them
				By("creating the Azure Blob Storage credentials", func() {
					AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds",
						azStorageAccount, azStorageKey)
				})

				// Create the cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzure, env)

				// Write a table and some data on the "app" database
				AssertCreateTestData(namespace, clusterName, tableName)
				AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// Create the backup
					utils.ExecuteBackup(namespace, sourceBackupFileAzure, env)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return utils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestore(namespace, externalClusterFileAzure, tableName)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				namespace = "recovery-pitr-barman-object-azure"
				clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileAzurePITR)
				Expect(err).ToNot(HaveOccurred())
				externalClusterName := "external-cluster-azure-pitr"

				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					clusterSourceFileAzurePITR,
					sourceBackupFileAzurePITR,
					azStorageAccount,
					azStorageKey,
					tableName,
					currentTimestamp)

				err = utils.CreateClusterFromExternalClusterBackupWithPITROnAzure(
					namespace, externalClusterName, clusterName, azStorageAccount, *currentTimestamp, env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestorePITR(namespace, externalClusterName, tableName)
			})
		})

		Context("storage account SAS Token authentication", func() {
			It("restores cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				namespace = "cluster-backup-azure-blob-sas"
				clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileAzureSAS)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())

				// The Azure Blob Storage should have been created ad-hoc for the test,
				// we get the credentials from the environment variables as we can't create
				// a fixture for them
				By("creating the Azure Blob Container SAS Token credentials", func() {
					AssertCreateSASTokenCredentials(namespace, azStorageAccount, azStorageKey)
				})

				// Create the Cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzureSAS, env)

				// Write a table and some data on the "app" database
				AssertCreateTestData(namespace, clusterName, tableName)

				// Create a WAL on the primary and check if it arrives on
				// Azure Blob Storage within a short time
				AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// We create a Backup
					utils.ExecuteBackup(namespace, sourceBackupFileAzureSAS, env)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return utils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restore backup in a new cluster
				AssertClusterRestore(namespace, clusterRestoreFileAzureSAS, tableName)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				namespace = "recovery-pitr-barman-object-azure-sas"
				clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileAzurePITRSAS)
				Expect(err).ToNot(HaveOccurred())
				externalClusterName := "external-cluster-azure-pitr"

				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())

				By("creating the Azure Blob Container SAS Token credentials", func() {
					AssertCreateSASTokenCredentials(namespace, azStorageAccount, azStorageKey)
				})

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					clusterSourceFileAzurePITRSAS,
					sourceBackupFileAzurePITRSAS,
					azStorageAccount,
					azStorageKey,
					tableName,
					currentTimestamp)

				err = utils.CreateClusterFromExternalClusterBackupWithPITROnAzure(
					namespace, externalClusterName, clusterName, azStorageAccount, *currentTimestamp, env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestorePITR(namespace, externalClusterName, tableName)
			})
		})
	})

	Context("using Azurite blobs as object storage", func() {
		It("restore cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Setup azurite and az cli along with Postgresql cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFileAzurite, tableName)

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, externalClusterFileAzurite, tableName)
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-pitr-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			externalClusterRestoreName := "restore-external-cluster-pitr"

			prepareClusterForPITROnAzurite(
				namespace,
				clusterName,
				azuriteBlobSampleFile,
				backupFileAzurite,
				tableName,
				currentTimestamp)

			//  Create a cluster from a particular time using external backup.
			err = utils.CreateClusterFromExternalClusterBackupWithPITROnAzurite(
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, externalClusterRestoreName, tableName)
		})
	})
})
