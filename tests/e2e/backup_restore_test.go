/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
)

const (
	minioClientName       = "mc"
	switchWalCmd          = "psql -U postgres app -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
	tableName             = "to_restore"
	azuriteBlobSampleFile = fixturesDir + "/backup/azurite/cluster-backup.yaml"
)

var namespace, clusterName, azStorageAccount, azStorageKey, currentTimeStamp string

var commandTimeout = time.Second * 5

var _ = Describe("Backup and restore", func() {
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
			CreateTestDataForTargetDB(namespace, clusterName, targetDBOne, testTableName)
			CreateTestDataForTargetDB(namespace, clusterName, targetDBTwo, testTableName)
			CreateTestDataForTargetDB(namespace, clusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				executeBackup(namespace, backupFile)

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
			AssertClusterRestore(namespace, clusterRestoreSampleFile)

			AssertMetricsData(namespace, restoredClusterName, targetDBOne, targetDBTwo, targetDBSecret)
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
				namespace, clusterName, clusterWithMinioSampleFile, backupFile)

			err = createClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, currentTimeStamp)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, restoredClusterName)
		})
	})

	Context("using azure blobs as object storage with storage account access authentication", func() {
		// We must be careful here. All the clusters use the same remote storage
		// and that means that we must use different cluster names otherwise
		// we risk mixing WALs and backups

		BeforeEach(func() {
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
				executeBackup(namespace, backupFile)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
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
			AssertClusterRestore(namespace, clusterRestoreSampleFile)
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
					return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
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
				return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
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
				azStorageKey)

			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

			err = createClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, currentTimeStamp)
			Expect(err).ToNot(HaveOccurred())

			// Restore backup in a new cluster
			AssertClusterRestorePITR(namespace, restoredClusterName)
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
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFile)

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile)
		})

		// We create a cluster, create a scheduled backup, patch it to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			namespace = "scheduled-backups-suspend-aurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			prepareClusterOnAzurite(namespace, clusterName, azuriteBlobSampleFile)

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				Eventually(func() (int, error) {
					return countFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
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
				return countFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 1))
		})

		It("backs up and restore a cluster with PITR", func() {
			namespace = "backup-restore-pitr-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			restoredClusterName := "restore-cluster-pitr"

			prepareClusterForPITROnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFile)

			err = createClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, currentTimeStamp)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, restoredClusterName)
		})
	})
})

var _ = Describe("Clusters Recovery From Barman Object Store", func() {
	const (
		fixturesBackupDir               = fixturesDir + "/backup/recovery_external_clusters/"
		externalClusterFileMinio        = fixturesBackupDir + "external-clusters-minio-03.yaml"
		externalClusterFileMinioReplica = fixturesBackupDir + "external-clusters-minio-replica-04.yaml"
		sourceBackupFileMinio           = fixturesBackupDir + "backup-minio-02.yaml"
		clusterSourceFileMinio          = fixturesBackupDir + "source-cluster-minio-01.yaml"
		sourceBackupFileAzure           = fixturesBackupDir + "backup-azure-blob-02.yaml"
		clusterSourceFileAzure          = fixturesBackupDir + "source-cluster-azure-blob-01.yaml"
		externalClusterFileAzure        = fixturesBackupDir + "external-clusters-azure-blob-03.yaml"
		clusterSourceFileAzurePITR      = fixturesBackupDir + "source-cluster-azure-blob-pitr.yaml"
		sourceBackupFileAzurePITR       = fixturesBackupDir + "backup-azure-blob-pitr.yaml"
		externalClusterFileAzurite      = fixturesBackupDir + "external-clusters-azurite-03.yaml"
		backupFileAzurite               = fixturesBackupDir + "backup-azurite-02.yaml"
		tableName                       = "to_restore"
		clusterSourceFileAzureSAS       = fixturesBackupDir + "cluster-with-backup-azure-blob-sas.yaml"
		clusterRestoreFileAzureSAS      = fixturesBackupDir + "cluster-from-restore-sas.yaml"
		sourceBackupFileAzureSAS        = fixturesBackupDir + "backup-azure-blob-sas.yaml"
		clusterSourceFileAzurePITRSAS   = fixturesBackupDir + "source-cluster-azure-blob-pitr-sas.yaml"
		sourceBackupFileAzurePITRSAS    = fixturesBackupDir + "backup-azure-blob-pitr-sas.yaml"
	)

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
				executeBackup(namespace, sourceBackupFileMinio)
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
			AssertClusterRestore(namespace, externalClusterFileMinio)

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
				namespace, clusterName, clusterSourceFileMinio, sourceBackupFileMinio)

			err = createClusterFromExternalClusterBackupWithPITROnMinio(
				namespace, externalClusterRestoreName, clusterName, currentTimeStamp)

			Expect(err).NotTo(HaveOccurred())
			AssertClusterRestorePITR(namespace, externalClusterRestoreName)
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
				executeBackup(namespace, sourceBackupFileMinio)
				Eventually(func() (int, error) {
					return CountFilesOnMinio(namespace, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
			})

			// Replicating a cluster with asynchronous replication
			AssertClusterAsyncReplica(namespace, clusterSourceFileMinio, externalClusterFileMinioReplica)
			// verify test data on restored external cluster
			externalClusterName, err := env.GetResourceNameFromYAML(externalClusterFileMinioReplica)
			Expect(err).ToNot(HaveOccurred())

			primaryPodInfo, err := env.GetClusterPrimary(namespace, externalClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertDataExpectedCount(namespace, primaryPodInfo.GetName(), tableName, 4)
		})
	})

	Context("using azure blobs as object storage", func() {
		BeforeEach(func() {
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
					executeBackup(namespace, sourceBackupFileAzure)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestore(namespace, externalClusterFileAzure)
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
					azStorageKey)

				err = createClusterFromExternalClusterBackupWithPITROnAzure(
					namespace, externalClusterName, clusterName, currentTimeStamp)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestorePITR(namespace, externalClusterName)
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
					createSASTokenCredentials(namespace, azStorageAccount, azStorageKey)
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
					executeBackup(namespace, sourceBackupFileAzureSAS)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restore backup in a new cluster
				AssertClusterRestore(namespace, clusterRestoreFileAzureSAS)
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
					createSASTokenCredentials(namespace, azStorageAccount, azStorageKey)
				})

				prepareClusterForPITROnAzureBlob(
					namespace,
					clusterName,
					clusterSourceFileAzurePITRSAS,
					sourceBackupFileAzurePITRSAS,
					azStorageAccount,
					azStorageKey)

				err = createClusterFromExternalClusterBackupWithPITROnAzure(
					namespace, externalClusterName, clusterName, currentTimeStamp)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestorePITR(namespace, externalClusterName)
			})
		})
	})

	Context("using Azurite blobs as object storage", func() {
		It("restore cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Setup azurite and az cli along with Postgresql cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFileAzurite)

			// Restore backup in a new cluster
			AssertClusterRestore(namespace, externalClusterFileAzurite)
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-pitr-azurite"
			clusterName, err := env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())
			externalClusterRestoreName := "restore-external-cluster-pitr"

			prepareClusterForPITROnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFileAzurite)

			//  Create a cluster from a particular time using external backup.
			err = createClusterFromExternalClusterBackupWithPITROnAzurite(
				namespace, externalClusterRestoreName, clusterName, currentTimeStamp)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, externalClusterRestoreName)
		})
	})
})

func createSASTokenCredentials(namespace string, id string, key string) {
	// Adding 24 hours to the current time
	date := time.Now().UTC().Add(time.Hour * 24)
	// Creating date time format for az command
	expiringDate := fmt.Sprintf("%v"+"-"+"%d"+"-"+"%v"+"T"+"%v"+":"+"%v"+"Z",
		date.Year(),
		date.Month(),
		date.Day(),
		date.Hour(),
		date.Minute())

	out, _, err := tests.Run(fmt.Sprintf(
		// SAS Token at Blob Container level does not currently work in Barman Cloud
		// https://github.com/EnterpriseDB/barman/issues/388
		// we will use SAS Token at Storage Account level
		// ( "az storage container generate-sas --account-name %v "+
		// "--name %v "+
		// "--https-only --permissions racwdl --auth-mode key --only-show-errors "+
		// "--expiry \"$(date -u -d \"+4 hours\" '+%%Y-%%m-%%dT%%H:%%MZ')\"",
		// id, blobContainerName )
		"az storage account generate-sas --account-name %v "+
			"--https-only --permissions cdlruwap --account-key %v "+
			"--resource-types co --services b --expiry %v -o tsv",
		id, key, expiringDate))
	Expect(err).ToNot(HaveOccurred())
	SASTokenRW := strings.TrimRight(out, "\n")

	out, _, err = tests.Run(fmt.Sprintf(
		"az storage account generate-sas --account-name %v "+
			"--https-only --permissions lr --account-key %v "+
			"--resource-types co --services b --expiry %v -o tsv",
		id, key, expiringDate))
	Expect(err).ToNot(HaveOccurred())
	SASTokenRO := strings.TrimRight(out, "\n")

	assertROSASTokenUnableToWrite("restore-cluster-sas", azStorageAccount, SASTokenRO)

	AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds-sas", id, SASTokenRW)
	AssertStorageCredentialsAreCreated(namespace, "restore-storage-creds-sas", id, SASTokenRO)
}

func assertROSASTokenUnableToWrite(containerName string, id string, key string) {
	_, _, err := tests.Run(fmt.Sprintf("az storage container create "+
		"--name %v --account-name %v "+
		"--sas-token %v", containerName, id, key))
	Expect(err).To(HaveOccurred())
}

func AssertScheduledBackupsAreScheduled(namespace string, backupYAMLPath string, timeout int) {
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, backupYAMLPath))
	Expect(err).NotTo(HaveOccurred())

	scheduledBackupName, err := env.GetResourceNameFromYAML(backupYAMLPath)
	Expect(err).NotTo(HaveOccurred())

	// We expect the scheduled backup to be scheduled before a
	// timeout
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}

	Eventually(func() (*v1.Time, error) {
		scheduledBackup := &apiv1.ScheduledBackup{}
		err := env.Client.Get(env.Ctx,
			scheduledBackupNamespacedName, scheduledBackup)
		return scheduledBackup.Status.LastScheduleTime, err
	}, timeout).ShouldNot(BeNil())

	// Within a few minutes we should have at least two backups
	Eventually(func() (int, error) {
		return getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
	}, timeout).Should(BeNumerically(">=", 2))
}

func AssertClusterAsyncReplica(namespace, sourceClusterFile, restoreClusterFile string) {
	By("Async Replication into external cluster", func() {
		restoredClusterName, err := env.GetResourceNameFromYAML(restoreClusterFile)
		Expect(err).ToNot(HaveOccurred())
		_, _, err = tests.Run(fmt.Sprintf(
			"kubectl apply -n %v -f %v",
			namespace, restoreClusterFile))
		Expect(err).ToNot(HaveOccurred())

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, 800, env)

		// Test data should be present on restored primary
		primaryReplica := restoredClusterName + "-1"
		postgresLogin := "psql -U postgres app -tAc "

		// Assert that the initial data is there from the create function
		cmd := postgresLogin + "'SELECT count(*) FROM to_restore'"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primaryReplica,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

		// Add additional data to the source cluster
		sourceClusterName, err := env.GetResourceNameFromYAML(sourceClusterFile)
		Expect(err).ToNot(HaveOccurred())

		AssertInsertTestData(namespace, sourceClusterName, "to_restore")

		// Assert that the new data is replicated
		cmd = postgresLogin + fmt.Sprintf("'SELECT count(*) FROM %v'", "to_restore")

		Eventually(func() (string, error) {
			out, _, err = tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryReplica,
				cmd))
			return strings.Trim(out, "\n"), err
		}, 300).Should(BeEquivalentTo("4"))

		// Cascading replicas should be attached to primary replica
		cmd = postgresLogin + "'SELECT count(*) FROM pg_stat_replication'"
		out, _, err = tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primaryReplica,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
	})
}

func AssertStorageCredentialsAreCreatedOnAzurite(namespace string) {
	// This is required by Azurite deployment
	secretFile := fixturesDir + "/backup/azurite/azurite-secret.yaml"
	_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, secretFile))
	Expect(err).ToNot(HaveOccurred())
}

func AssertClusterRestore(namespace string, restoreClusterFile string) {
	By("Restoring a backup in a new cluster", func() {
		restoredClusterName, err := env.GetResourceNameFromYAML(restoreClusterFile)
		Expect(err).ToNot(HaveOccurred())
		_, _, err = tests.Run(fmt.Sprintf(
			"kubectl apply -n %v -f %v",
			namespace, restoreClusterFile))
		Expect(err).ToNot(HaveOccurred())

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, 800, env)

		// Test data should be present on restored primary
		primary := restoredClusterName + "-1"
		AssertDataExpectedCount(namespace, primary, tableName, 2)

		// Restored primary should be on timeline 2
		cmd := "psql -U postgres app -tAc 'select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)'"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

		// Restored standby should be attached to restored primary
		assertClusterStandbysAreStreaming(namespace, restoredClusterName)
	})
}

func AssertScheduledBackupsImmediate(namespace, backupYAMLPath, scheduledBackupName string) {
	By("scheduling immediate backups", func() {
		var err error
		// Create the ScheduledBackup
		_, _, err = tests.Run(fmt.Sprintf(
			"kubectl apply -n %v -f %v",
			namespace, backupYAMLPath))
		Expect(err).NotTo(HaveOccurred())

		// We expect the scheduled backup to be scheduled after creation
		scheduledBackupNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      scheduledBackupName,
		}
		Eventually(func() (*v1.Time, error) {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx,
				scheduledBackupNamespacedName, scheduledBackup)
			return scheduledBackup.Status.LastScheduleTime, err
		}, 30).ShouldNot(BeNil())

		// backup count should be 1 that is immediate one
		Eventually(func() (int, error) {
			currentBackupCount, err := getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return currentBackupCount, err
		}, 60).Should(BeNumerically("==", 1))
	})
}

func executeBackup(namespace string, backupFile string) {
	backupName, err := env.GetResourceNameFromYAML(backupFile)
	Expect(err).ToNot(HaveOccurred())

	_, _, err = tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, backupFile))
	Expect(err).ToNot(HaveOccurred())

	// After a while the Backup should be completed
	timeout := 180
	backupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      backupName,
	}
	backup := &apiv1.Backup{}
	// Verifying backup status
	Eventually(func() (apiv1.BackupPhase, error) {
		err = env.Client.Get(env.Ctx, backupNamespacedName, backup)
		return backup.Status.Phase, err
	}, timeout).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
	Eventually(func() (string, error) {
		err = env.Client.Get(env.Ctx, backupNamespacedName, backup)
		if err != nil {
			return "", err
		}
		backupStatus := backup.GetStatus()
		return backupStatus.BeginLSN, err
	}, timeout).ShouldNot(BeEmpty())

	backupStatus := backup.GetStatus()
	Expect(backupStatus.BeginWal).NotTo(BeEmpty())
	Expect(backupStatus.EndLSN).NotTo(BeEmpty())
	Expect(backupStatus.EndWal).NotTo(BeEmpty())
}

func getScheduledBackupCompleteBackupsCount(namespace string, scheduledBackupName string) (int, error) {
	backups, err := getScheduledBackupBackups(namespace, scheduledBackupName)
	if err != nil {
		return -1, err
	}
	completed := 0
	for _, backup := range backups {
		if strings.HasPrefix(backup.Name, scheduledBackupName+"-") &&
			backup.Status.Phase == apiv1.BackupPhaseCompleted {
			completed++
		}
	}
	return completed, nil
}

func AssertSuspendScheduleBackups(namespace, scheduledBackupName string) {
	var completedBackupsCount int
	var err error
	By("suspending the scheduled backup", func() {
		// update suspend status to true
		cmd := fmt.Sprintf("kubectl patch ScheduledBackup %v -n %v -p '{\"spec\":{\"suspend\":true}}' "+
			"--type='merge'", scheduledBackupName, namespace)
		_, _, err = tests.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
		scheduledBackupNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      scheduledBackupName,
		}
		Eventually(func() bool {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx, scheduledBackupNamespacedName, scheduledBackup)
			return *scheduledBackup.Spec.Suspend
		}, 30).Should(BeTrue())
	})
	By("waiting for ongoing backup to complete", func() {
		Eventually(func() (bool, error) {
			completedBackupsCount, err = getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
			if err != nil {
				return false, err
			}
			backups, err := getScheduledBackupBackups(namespace, scheduledBackupName)
			if err != nil {
				return false, err
			}
			return len(backups) == completedBackupsCount, nil
		}, 60).Should(BeTrue())
	})
	By("verifying backup has suspended", func() {
		Consistently(func() (int, error) {
			backups, err := getScheduledBackupBackups(namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return len(backups), err
		}, 40).Should(BeEquivalentTo(completedBackupsCount))
	})
	By("resuming suspended backup", func() {
		// take current backup count before suspend the schedule backup
		completedBackupsCount, err = getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
		Expect(err).ToNot(HaveOccurred())

		cmd := fmt.Sprintf("kubectl patch ScheduledBackup %v -n %v -p '{\"spec\":{\"suspend\":false}}' "+
			"--type='merge'", scheduledBackupName, namespace)
		_, _, err = tests.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
	})
	By("verifying backup has resumed", func() {
		Eventually(func() (int, error) {
			currentBackupCount, err := getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return currentBackupCount, err
		}, 70).Should(BeNumerically(">", completedBackupsCount))
	})
}

func AssertClusterRestorePITR(namespace, clusterName string) {
	primaryInfo := &corev1.Pod{}
	var err error

	By("restoring a backup cluster with PITR in a new cluster", func() {
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, clusterName, 800, env)

		primaryInfo, err = env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		// Restored primary should be on timeline 2
		query := "select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)"
		stdOut, _, err := env.ExecCommand(env.Ctx, *primaryInfo, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Trim(stdOut, "\n"), err).To(Equal("00000002"))

		// Restored standby should be attached to restored primary
		query = "SELECT count(*) FROM pg_stat_replication"
		stdOut, _, err = env.ExecCommand(env.Ctx, *primaryInfo, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Trim(stdOut, "\n"), err).To(BeEquivalentTo("2"))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		// Only 2 entries should be present
		AssertDataExpectedCount(namespace, primaryInfo.GetName(), tableName, 2)
	})
}

func AssertArchiveWalOnAzurite(namespace, clusterName string) {
	// Create a WAL on the primary and check if it arrives at the Azure Blob Storage within a short time
	By("archiving WALs and verifying they exist", func() {
		primary := clusterName + "-1"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			switchWalCmd))
		Expect(err).ToNot(HaveOccurred())

		latestWAL := strings.TrimSpace(out)
		// verifying on blob storage using az
		// Define what file we are looking for in Azurite.
		// Escapes are required since az expects forward slashes to be escaped
		path := fmt.Sprintf("%v\\/wals\\/0000000100000000\\/%v.gz", clusterName, latestWAL)
		// verifying on blob storage using az
		Eventually(func() (int, error) {
			return countFilesOnAzuriteBlobStorage(namespace, clusterName, path)
		}, 60).Should(BeEquivalentTo(1))
	})
}

func AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey string) {
	// Create a WAL on the primary and check if it arrives at the Azure Blob Storage, within a short time
	By("archiving WALs and verifying they exist", func() {
		primary := clusterName + "-1"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			switchWalCmd))
		Expect(err).ToNot(HaveOccurred())

		latestWAL := strings.TrimSpace(out)
		// Define what file we are looking for in Azure.
		// Escapes are required since az expects forward slashes to be escaped
		path := fmt.Sprintf("%v\\/wals\\/0000000100000000\\/%v.gz", clusterName, latestWAL)
		// Verifying on blob storage using az
		Eventually(func() (int, error) {
			return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, path)
		}, 30).Should(BeEquivalentTo(1))
	})
}

func composeAzBlobListAzuriteCmd(clusterName string, path string) string {
	return fmt.Sprintf("az storage blob list --container-name %v --query \"[?contains(@.name, \\`%v\\`)].name\" "+
		"--connection-string $AZURE_CONNECTION_STRING",
		clusterName, path)
}

func getScheduledBackupBackups(namespace string, scheduledBackupName string) ([]apiv1.Backup, error) {
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}
	// Get all the backups that are children of the ScheduledBackup
	scheduledBackup := &apiv1.ScheduledBackup{}
	err := env.Client.Get(env.Ctx, scheduledBackupNamespacedName,
		scheduledBackup)
	backups := &apiv1.BackupList{}
	if err != nil {
		return nil, err
	}
	err = env.Client.List(env.Ctx, backups,
		ctrlclient.InNamespace(namespace))
	if err != nil {
		return nil, err
	}
	ret := []apiv1.Backup{}

	for _, backup := range backups.Items {
		if strings.HasPrefix(backup.Name, scheduledBackup.Name+"-") {
			ret = append(ret, backup)
		}
	}
	return ret, nil
}

func composeFindMinioCmd(path string, serviceName string) string {
	return fmt.Sprintf("sh -c 'mc find %v --name %v | wc -l'", serviceName, path)
}

func composeAzBlobListCmd(azStorageAccount, azStorageKey, clusterName string, path string) string {
	return fmt.Sprintf("az storage blob list --account-name %v  "+
		"--account-key %v  "+
		"--container-name %v --query \"[?contains(@.name, \\`%v\\`)].name\"",
		azStorageAccount, azStorageKey, clusterName, path)
}

func countFilesOnAzureBlobStorage(
	azStorageAccount string,
	azStorageKey string,
	clusterName string,
	path string) (int, error) {
	azBlobListCmd := composeAzBlobListCmd(azStorageAccount, azStorageKey, clusterName, path)
	out, _, err := tests.RunUnchecked(azBlobListCmd)
	if err != nil {
		return -1, err
	}
	var arr []string
	err = json.Unmarshal([]byte(out), &arr)
	return len(arr), err
}

func countFilesOnAzuriteBlobStorage(
	namespace,
	clusterName string,
	path string) (int, error) {
	azBlobListCmd := composeAzBlobListAzuriteCmd(clusterName, path)
	out, _, err := tests.RunUnchecked(fmt.Sprintf("kubectl exec -n %v az-cli "+
		"-- /bin/bash -c '%v'", namespace, azBlobListCmd))
	if err != nil {
		return -1, err
	}
	var arr []string
	err = json.Unmarshal([]byte(out), &arr)
	return len(arr), err
}

func installAzurite(namespace string) {
	// Create an Azurite for blob storage
	azuriteDeploymentFile := fixturesDir +
		"/backup/azurite/azurite-deployment.yaml"

	_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, azuriteDeploymentFile))
	Expect(err).ToNot(HaveOccurred())

	// Wait for the Azurite pod to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "azurite",
	}
	Eventually(func() (int32, error) {
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
		return deployment.Status.ReadyReplicas, err
	}, 300).Should(BeEquivalentTo(1))

	// Create an Azurite service
	serviceFile := fixturesDir + "/backup/azurite/azurite-service.yaml"
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, serviceFile))
	Expect(err).ToNot(HaveOccurred())
}

func installAzCli(namespace string) {
	clientFile := fixturesDir + "/backup/azurite/az-cli.yaml"
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, clientFile))
	Expect(err).ToNot(HaveOccurred())
	azCliNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "az-cli",
	}
	Eventually(func() (bool, error) {
		az := &corev1.Pod{}
		err = env.Client.Get(env.Ctx, azCliNamespacedName, az)
		return utils.IsPodReady(*az), err
	}, 180).Should(BeTrue())
}

func createClusterFromBackupUsingPITR(namespace, clusterName, backupFilePath, targetTime string) error {
	backupName, err := env.GetResourceNameFromYAML(backupFilePath)
	if err != nil {
		return err
	}
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	restoreCluster := &apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: apiv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_checkpoints":             "on",
					"log_lock_waits":              "on",
					"log_min_duration_statement":  "1000",
					"log_statement":               "ddl",
					"log_temp_files":              "1024",
					"log_autovacuum_min_duration": "1s",
					"log_replication_commands":    "on",
				},
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					Backup: &apiv1.LocalObjectReference{
						Name: backupName,
					},
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},
		},
	}
	return env.Client.Create(env.Ctx, restoreCluster)
}

func createClusterFromExternalClusterBackupWithPITROnAzure(
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string) error {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	destinationPath := fmt.Sprintf("https://%v.blob.core.windows.net/%v/", azStorageAccount, sourceClusterName)

	restoreCluster := &apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: apiv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_checkpoints":             "on",
					"log_lock_waits":              "on",
					"log_min_duration_statement":  "1000",
					"log_statement":               "ddl",
					"log_temp_files":              "1024",
					"log_autovacuum_min_duration": "1s",
					"log_replication_commands":    "on",
				},
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: destinationPath,
						AzureCredentials: &apiv1.AzureCredentials{
							StorageAccount: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "backup-storage-creds",
								},
								Key: "ID",
							},
							StorageKey: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "backup-storage-creds",
								},
								Key: "KEY",
							},
						},
					},
				},
			},
		},
	}

	return env.Client.Create(env.Ctx, restoreCluster)
}

func createClusterFromExternalClusterBackupWithPITROnMinio(
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string) error {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")

	restoreCluster := &apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: apiv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_checkpoints":             "on",
					"log_lock_waits":              "on",
					"log_min_duration_statement":  "1000",
					"log_statement":               "ddl",
					"log_temp_files":              "1024",
					"log_autovacuum_min_duration": "1s",
					"log_replication_commands":    "on",
				},
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: "s3://cluster-backups/",
						EndpointURL:     "http://minio-service:9000",
						S3Credentials: &apiv1.S3Credentials{
							AccessKeyIDReference: apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "backup-storage-creds",
								},
								Key: "ID",
							},
							SecretAccessKeyReference: apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "backup-storage-creds",
								},
								Key: "KEY",
							},
						},
					},
				},
			},
		},
	}

	return env.Client.Create(env.Ctx, restoreCluster)
}

func createClusterFromExternalClusterBackupWithPITROnAzurite(
	namespace,
	externalClusterName,
	sourceClusterName,
	targetTime string) error {
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	DestinationPath := fmt.Sprintf("http://azurite:10000/storageaccountname/%v", sourceClusterName)

	restoreCluster := &apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      externalClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			PostgresConfiguration: apiv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_checkpoints":             "on",
					"log_lock_waits":              "on",
					"log_min_duration_statement":  "1000",
					"log_statement":               "ddl",
					"log_temp_files":              "1024",
					"log_autovacuum_min_duration": "1s",
					"log_replication_commands":    "on",
				},
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{
					Source: sourceClusterName,
					RecoveryTarget: &apiv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name: sourceClusterName,
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: DestinationPath,
						AzureCredentials: &apiv1.AzureCredentials{
							ConnectionString: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "azurite",
								},
								Key: "AZURE_CONNECTION_STRING",
							},
						},
					},
				},
			},
		},
	}

	return env.Client.Create(env.Ctx, restoreCluster)
}

// getCurrentTimeStamp getting current time stamp from postgres server
func getCurrentTimeStamp(namespace, clusterName string) (string, error) {
	primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
	if err != nil {
		return "", err
	}

	query := "select CURRENT_TIMESTAMP;"
	stdOut, _, err := env.ExecCommand(env.Ctx, *primaryPodInfo, "postgres",
		&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
	if err != nil {
		return "", err
	}

	currentTimeStamp = strings.Trim(stdOut, "\n")
	return currentTimeStamp, nil
}

func prepareClusterForPITROnMinio(
	namespace,
	clusterName,
	clusterSampleFile,
	backupSampleFile string) {
	err := env.CreateNamespace(namespace)
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
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

	By("backing up a cluster and verifying it exists on minio", func() {
		executeBackup(namespace, backupSampleFile)

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

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(namespace, clusterName, tableName)

	By("getting currentTimestamp", func() {
		currentTimeStamp, err = getCurrentTimeStamp(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableName), func() {
		err = insertRecordIntoTable(namespace, clusterName, tableName, 3)
		Expect(err).ToNot(HaveOccurred())
	})
	AssertArchiveWalOnMinio(namespace, clusterName)
}

func prepareClusterForPITROnAzureBlob(
	namespace,
	clusterName,
	clusterSampleFile,
	backupSampleFile string,
	azStorageAccount string,
	azStorageKey string) {
	var err error
	// The Azure Blob Storage should have been created ad-hoc for the test.
	// The credentials are retrieved from the environment variables, as we can't create
	// a fixture for them
	By("creating the Azure Blob Storage credentials", func() {
		AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", azStorageAccount, azStorageKey)
	})

	// Create the cluster
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

	By("backing up a cluster and verifying it exists on Azure Blob", func() {
		executeBackup(namespace, backupSampleFile)

		Eventually(func() (int, error) {
			return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
		}, 30).Should(BeEquivalentTo(1))
		Eventually(func() (string, error) {
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
				cluster)
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(namespace, clusterName, tableName)

	By("getting currentTimestamp", func() {
		currentTimeStamp, err = getCurrentTimeStamp(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableName), func() {
		err = insertRecordIntoTable(namespace, clusterName, tableName, 3)
		Expect(err).ToNot(HaveOccurred())
	})
	AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)
}

func prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile string) {
	// Create a cluster in a namespace we'll delete after the test
	err := env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())

	By("creating the Azurite storage credentials", func() {
		AssertStorageCredentialsAreCreatedOnAzurite(namespace)
	})

	By("setting up Azurite to hold the backups", func() {
		// Deploying azurite for blob storage
		installAzurite(namespace)
	})

	By("setting up az-cli", func() {
		// This is required as we have a service of Azurite running locally.
		// In order to connect, we need az cli inside the namespace
		installAzCli(namespace)
	})

	// Creating cluster
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)
}

func prepareClusterBackupOnAzurite(namespace, clusterName, clusterSampleFile, backupFile string) {
	// Setting up Azurite and az cli along with Postgresql cluster
	prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile)
	// Write a table and some data on the "app" database
	AssertCreateTestData(namespace, clusterName, tableName)
	AssertArchiveWalOnAzurite(namespace, clusterName)

	By("backing up a cluster and verifying it exists on azurite", func() {
		// We create a Backup
		executeBackup(namespace, backupFile)
		// Verifying file called data.tar should be available on Azurite blob storage
		Eventually(func() (int, error) {
			return countFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
		}, 30).Should(BeNumerically(">=", 1))
		Eventually(func() (string, error) {
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
				cluster)
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})
}

func prepareClusterForPITROnAzurite(
	namespace,
	clusterName,
	clusterSampleFile,
	backupSampleFile string) {
	prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile)

	By("backing up a cluster and verifying it exists on azurite", func() {
		// We create a Backup
		executeBackup(namespace, backupSampleFile)
		// Verifying file called data.tar should be available on Azurite blob storage
		Eventually(func() (int, error) {
			return countFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
		}, 30).Should(BeNumerically(">=", 1))
		Eventually(func() (string, error) {
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
				cluster)
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(namespace, clusterName, tableName)

	By("getting currentTimestamp", func() {
		var err error
		currentTimeStamp, err = getCurrentTimeStamp(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableName), func() {
		err := insertRecordIntoTable(namespace, clusterName, tableName, 3)
		Expect(err).ToNot(HaveOccurred())
	})
	AssertArchiveWalOnAzurite(namespace, clusterName)
}
