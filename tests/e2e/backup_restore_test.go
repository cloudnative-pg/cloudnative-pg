/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	minioClientName = "mc"
	switchWalCmd    = "psql -U postgres app -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
	tableName       = "to_restore"
)

var namespace, clusterName, azStorageAccount, azStorageKey, currentTimeStamp string

var commandTimeout = time.Second * 5

var _ = Describe("Backup and restore", func() {
	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
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
				assertStorageCredentialsAreCreated(namespace, "minio", "minio123")
			})

			By("setting up minio", func() {
				installMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				installMinioClient(namespace)
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

			assertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				executeBackup(namespace, backupFile)

				Eventually(func() (int, error) {
					return countFilesOnMinio(namespace, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
			})

			// Restore backup in a new cluster
			assertClusterRestore(namespace, clusterRestoreSampleFile)

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
				assertStorageCredentialsAreCreated(namespace, "minio", "minio123")
			})

			By("setting up minio", func() {
				installMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				installMinioClient(namespace)
			})

			AssertCreateCluster(namespace, clusterName, clusterWithMinioSampleFile, env)

			By("scheduling backups", func() {
				assertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				Eventually(func() (int, error) {
					return countFilesOnMinio(namespace, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})

			assertSuspendScheduleBackups(namespace, scheduledBackupName)
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
				assertStorageCredentialsAreCreated(namespace, "minio", "minio123")
			})

			By("setting up minio", func() {
				installMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				installMinioClient(namespace)
			})

			AssertCreateCluster(namespace, clusterName, clusterWithMinioSampleFile, env)

			assertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

			// assertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return countFilesOnMinio(namespace, "data.tar")
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

			assertClusterRestorePITR(namespace, restoredClusterName)
		})
	})
	Context("using azure blobs as object storage", func() {
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
				assertStorageCredentialsAreCreated(namespace, azStorageAccount, azStorageKey)
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, azureBlobSampleFile, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)
			assertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)
			By("uploading a backup", func() {
				// We create a backup
				backupFile := fixturesDir + "/backup/azure_blob/backup-azure-blob.yaml"
				executeBackup(namespace, backupFile)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
				}, 30).Should(BeNumerically(">=", 1))
			})

			// Restore backup in a new cluster
			assertClusterRestore(namespace, clusterRestoreSampleFile)
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
				assertStorageCredentialsAreCreated(namespace, azStorageAccount, azStorageKey)
			})

			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("scheduling backups", func() {
				assertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 480)

				// assertScheduledBackupsImmediate creates at least two backups, we should find
				// their base backups
				Eventually(func() (int, error) {
					return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})
			assertSuspendScheduleBackups(namespace, scheduledBackupName)
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
				assertStorageCredentialsAreCreated(namespace, azStorageAccount, azStorageKey)
			})

			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			assertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

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

			prepareClusterForPITROnAzureBlob(
				namespace,
				clusterName,
				azureBlobSampleFile,
				backupFile,
				azStorageAccount,
				azStorageKey)

			assertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

			err = createClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, currentTimeStamp)
			Expect(err).ToNot(HaveOccurred())

			// Restore backup in a new cluster
			assertClusterRestorePITR(namespace, restoredClusterName)
		})
	})
	Context("using Azurite blobs as object storage", func() {
		BeforeEach(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if isAKS {
				Skip("This test is only executed on gke, openshift and local")
			}
		})

		It("restores a backed up cluster", func() {
			namespace = "cluster-backup-azurite"
			clusterName = "pg-backup-azurite"
			const azuriteBlobSampleFile = fixturesDir + "/backup/azurite/cluster-backup.yaml"
			const clusterRestoreSampleFile = fixturesDir + "/backup/azurite/cluster-from-restore.yaml"
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating the Azurite storage credentials", func() {
				// This is required by Azurite deployment
				secretFile := fixturesDir + "/backup/azurite/azurite-secret.yaml"
				_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
					namespace, secretFile))
				Expect(err).ToNot(HaveOccurred())
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

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, azuriteBlobSampleFile, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

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

			By("uploading a backup", func() {
				// Create the backup
				executeBackup(namespace, fixturesDir+"/backup/azurite/backup.yaml")

				// Verifying file called data.tar should be available on Azurite blob storage
				Eventually(func() (int, error) {
					return countFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
				}, 30).Should(BeNumerically(">=", 1))
			})

			// Restore backup in a new cluster
			assertClusterRestore(namespace, clusterRestoreSampleFile)
		})
	})
})

var _ = Describe("Clusters Recovery From Barman Object Store", func() {
	const (
		fixturesBackupDir          = fixturesDir + "/backup/recovery_external_clusters/"
		externalClusterFileMinio   = fixturesBackupDir + "external-clusters-minio-03.yaml"
		sourceBackupFileMinio      = fixturesBackupDir + "backup-minio-02.yaml"
		clusterSourceFileMinio     = fixturesBackupDir + "source-cluster-minio-01.yaml"
		sourceBackupFileAzure      = fixturesBackupDir + "backup-azure-blob-02.yaml"
		clusterSourceFileAzure     = fixturesBackupDir + "source-cluster-azure-blob-01.yaml"
		externalClusterFileAzure   = fixturesBackupDir + "external-clusters-azure-blob-03.yaml"
		clusterSourceFileAzurePITR = fixturesBackupDir + "source-cluster-azure-blob-pitr.yaml"
		sourceBackupFileAzurePITR  = fixturesBackupDir + "backup-azure-blob-pitr.yaml"
	)

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().FullTestText+".log")
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
				assertStorageCredentialsAreCreated(namespace, "minio", "minio123")
			})
			By("setting up minio", func() {
				installMinio(namespace)
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				installMinioClient(namespace)
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterSourceFileMinio, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			assertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				executeBackup(namespace, sourceBackupFileMinio)
				Eventually(func() (int, error) {
					return countFilesOnMinio(namespace, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
			})

			// Restoring cluster using a recovery barman object store, which is defined
			// in the externalClusters section
			assertClusterRestore(namespace, externalClusterFileMinio)

			// verify test data on restored external cluster
			primaryPodInfo, err := env.GetClusterPrimary(namespace, externalClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertTestDataExpectedCount(namespace, primaryPodInfo.GetName(), tableName, 2)
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
			assertClusterRestorePITR(namespace, externalClusterRestoreName)
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

		It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			namespace = "recovery-barman-object-azure"
			clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileAzure)
			Expect(err).ToNot(HaveOccurred())
			externalClusterName, err := env.GetResourceNameFromYAML(externalClusterFileAzure)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// The Azure Blob Storage should have been created ad-hoc for the test.
			// The credentials are retrieved from the environment variables, as we can't create
			// a fixture for them
			By("creating the Azure Blob Storage credentials",
				func() {
					assertStorageCredentialsAreCreated(namespace, azStorageAccount, azStorageKey)
				})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, clusterSourceFileAzure, env)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)
			assertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

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
			assertClusterRestore(namespace, externalClusterFileAzure)

			// verify test data on restored external cluster
			primaryPodInfo, err := env.GetClusterPrimary(namespace, externalClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertTestDataExpectedCount(namespace, primaryPodInfo.GetName(), tableName, 2)
		})

		It("restores a cluster with 'PITR' from barman object using "+
			"'barmanObjectStore' option in 'externalClusters' section", func() {
			namespace = "recovery-pitr-barman-object-azure"
			clusterName, err := env.GetResourceNameFromYAML(clusterSourceFileAzurePITR)
			Expect(err).ToNot(HaveOccurred())
			externalClusterName := "external-cluster-azure-pitr"

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
			assertClusterRestorePITR(namespace, externalClusterName)
		})
	})
})

func assertScheduledBackupsAreScheduled(namespace string, backupYAMLPath string, timeout int) {
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

func assertStorageCredentialsAreCreated(namespace string, id string, key string) {
	_, _, err := tests.Run(fmt.Sprintf("kubectl create secret generic backup-storage-creds -n %v "+
		"--from-literal=ID=%v "+
		"--from-literal=KEY=%v",
		namespace, id, key))
	Expect(err).ToNot(HaveOccurred())
}

func assertClusterRestore(namespace string, restoreClusterFile string) {
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
		postgresLogin := "psql -U postgres app -tAc "

		cmd := postgresLogin + "'SELECT count(*) FROM to_restore'"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))

		// Restored primary should be on timeline 2
		cmd = postgresLogin + "'select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)'"
		out, _, err = tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

		// Restored standby should be attached to restored primary
		cmd = postgresLogin + "'SELECT count(*) FROM pg_stat_replication'"
		out, _, err = tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			cmd))
		Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
	})
}

func installMinio(namespace string) {
	// Create a PVC-based deployment for the minio version
	// minio/minio:RELEASE.2020-04-23T00-58-49Z
	minioPVCFile := fixturesDir + "/backup/minio/minio-pvc.yaml"
	minioDeploymentFile := fixturesDir +
		"/backup/minio/minio-deployment.yaml"

	_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, minioPVCFile))
	Expect(err).ToNot(HaveOccurred())
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, minioDeploymentFile))
	Expect(err).ToNot(HaveOccurred())

	// Wait for the minio pod to be ready
	deploymentName := "minio"
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      deploymentName,
	}
	Eventually(func() (int32, error) {
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
		return deployment.Status.ReadyReplicas, err
	}, 300).Should(BeEquivalentTo(1))

	// Create a minio service
	serviceFile := fixturesDir + "/backup/minio/minio-service.yaml"
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, serviceFile))
	Expect(err).ToNot(HaveOccurred())
}

func installMinioClient(namespace string) {
	clientFile := fixturesDir + "/backup/minio/minio-client.yaml"
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, clientFile))
	Expect(err).ToNot(HaveOccurred())
	mcNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      minioClientName,
	}
	Eventually(func() (bool, error) {
		mc := &corev1.Pod{}
		err = env.Client.Get(env.Ctx, mcNamespacedName, mc)
		return utils.IsPodReady(*mc), err
	}, 180).Should(BeTrue())
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

func assertScheduledBackupsImmediate(namespace, backupYAMLPath, scheduledBackupName string) {
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

func assertSuspendScheduleBackups(namespace, scheduledBackupName string) {
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

func composeFindMinioCmd(path string, serviceName string) string {
	return fmt.Sprintf("sh -c 'mc find %v --name %v | wc -l'", serviceName, path)
}

// Use the minioClient `minioClientName` in namespace `namespace` to count  the amount of files matching the `path`
func countFilesOnMinio(namespace string, path string) (int, error) {
	out, _, err := tests.RunUnchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		minioClientName,
		composeFindMinioCmd(path, "minio")))
	if err != nil {
		return -1, err
	}
	value, err := strconv.Atoi(strings.Trim(out, "\n"))
	return value, err
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

func assertArchiveWalOnMinio(namespace, clusterName string) {
	// Create a WAL on the primary and check if it arrives at minio, within a short time
	By("archiving WALs and verifying they exist", func() {
		primary := clusterName + "-1"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			switchWalCmd))
		Expect(err).ToNot(HaveOccurred())

		latestWAL := strings.TrimSpace(out)
		Eventually(func() (int, error) {
			// WALs are compressed with gzip in the fixture
			return countFilesOnMinio(namespace, latestWAL+".gz")
		}, 30).Should(BeEquivalentTo(1))
	})
}

func assertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey string) {
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
					"log_line_prefix":             "%m [%p]: u=[%u] db=[%d] app=[%a] c=[%h] s=[%c:%l] tx=[%v:%x]",
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
					"log_line_prefix":             "%m [%p]: u=[%u] db=[%d] app=[%a] c=[%h] s=[%c:%l] tx=[%v:%x]",
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
					"log_line_prefix":             "%m [%p]: u=[%u] db=[%d] app=[%a] c=[%h] s=[%c:%l] tx=[%v:%x]",
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

func assertClusterRestorePITR(namespace, clusterName string) {
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
		AssertTestDataExpectedCount(namespace, primaryInfo.GetName(), tableName, 2)
	})
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

// insertRecordIntoTestTable insert an entry entry into test table
func insertRecordIntoTestTable(namespace, clusterName, tableName string, value int) error {
	primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
	if err != nil {
		return err
	}

	query := fmt.Sprintf("INSERT INTO %v VALUES (%v);", tableName, value)
	_, _, err = env.ExecCommand(env.Ctx, *primaryPodInfo, "postgres",
		&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
	if err != nil {
		return err
	}

	return nil
}

func prepareClusterForPITROnMinio(
	namespace,
	clusterName,
	clusterSampleFile,
	backupSampleFile string) {
	err := env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())

	By("creating the credentials for minio", func() {
		assertStorageCredentialsAreCreated(namespace, "minio", "minio123")
	})

	By("setting up minio", func() {
		installMinio(namespace)
	})

	// Create the minio client pod and wait for it to be ready.
	// We'll use it to check if everything is archived correctly
	By("setting up minio client pod", func() {
		installMinioClient(namespace)
	})

	// Create the cluster
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

	By("backing up a cluster and verifying it exists on minio", func() {
		executeBackup(namespace, backupSampleFile)

		Eventually(func() (int, error) {
			return countFilesOnMinio(namespace, "data.tar")
		}, 30).Should(BeEquivalentTo(1))
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(namespace, clusterName, tableName)

	By("getting currentTimestamp", func() {
		currentTimeStamp, err = getCurrentTimeStamp(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableName), func() {
		err = insertRecordIntoTestTable(namespace, clusterName, tableName, 3)
		Expect(err).ToNot(HaveOccurred())
	})
	assertArchiveWalOnMinio(namespace, clusterName)
}

func prepareClusterForPITROnAzureBlob(
	namespace,
	clusterName,
	clusterSampleFile,
	backupSampleFile string,
	azStorageAccount string,
	azStorageKey string) {
	err := env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())

	// The Azure Blob Storage should have been created ad-hoc for the test.
	// The credentials are retrieved from the environment variables, as we can't create
	// a fixture for them
	By("creating the Azure Blob Storage credentials", func() {
		assertStorageCredentialsAreCreated(namespace, azStorageAccount, azStorageKey)
	})

	// Create the cluster
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

	By("backing up a cluster and verifying it exists on Azure Blob", func() {
		executeBackup(namespace, backupSampleFile)

		Eventually(func() (int, error) {
			return countFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
		}, 30).Should(BeEquivalentTo(1))
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(namespace, clusterName, tableName)

	By("getting currentTimestamp", func() {
		currentTimeStamp, err = getCurrentTimeStamp(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableName), func() {
		err = insertRecordIntoTestTable(namespace, clusterName, tableName, 3)
		Expect(err).ToNot(HaveOccurred())
	})
	assertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)
}
