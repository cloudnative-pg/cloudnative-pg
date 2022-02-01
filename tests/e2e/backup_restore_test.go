/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"os"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	testUtils "github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup and restore", Label(tests.LabelBackupRestore), func() {
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
	Context("using minio as object storage", Ordered, func() {
		// This is a set of tests using a minio server deployed in the same
		// namespace as the cluster. Since each cluster is installed in its
		// own namespace, they can share the configuration file

		const (
			backupFile                 = fixturesDir + "/backup/minio/backup-minio.yaml"
			clusterWithMinioSampleFile = fixturesDir + "/backup/minio/cluster-with-backup-minio.yaml"
			customQueriesSampleFile    = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
			minioCaSecName             = "minio-server-ca-secret"
			minioTLSSecName            = "minio-server-tls-secret"
		)
		BeforeAll(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if isAKS {
				Skip("Test is not run on AKS.")
			}
			namespace = "cluster-backup-minio"
			clusterName, err = env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating ca and tls certificate secrets", func() {
				// create CA certificates
				_, caPair := testUtils.CreateSecretCA(namespace, clusterName, minioCaSecName, true, env)

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
				err = testUtils.InstallMinio(env, setup, 300)
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				minioClient := testUtils.MinioSSLClient(namespace)
				err := testUtils.PodCreateAndWaitForReady(env, &minioClient, 240)
				Expect(err).ToNot(HaveOccurred())
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

		AfterAll(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			const (
				targetDBOne              = "test"
				targetDBTwo              = "test1"
				targetDBSecret           = "secret_test"
				testTableName            = "test_table"
				clusterRestoreSampleFile = fixturesDir + "/backup/cluster-from-restore.yaml"
			)

			restoredClusterName, err := env.GetResourceNameFromYAML(clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			// Create required test data
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBOne, testTableName)
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBTwo, testTableName)
			AssertCreationOfTestDataForTargetDB(namespace, clusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, backupFile, env)

				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, "data.tar")
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
				previous, err = testUtils.CountFilesOnMinio(namespace, minioClientName, "*.history.gz")
				Expect(err).ToNot(HaveOccurred())
			})

			AssertSwitchover(namespace, clusterName, env)

			By("checking the number of .history after switchover", func() {
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, "*.history.gz")
				}, 60).Should(BeNumerically(">", previous))
			})
		})

		// Create a scheduled backup with the 'immediate' option enabled. We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups 'immediate' option", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-minio.yaml"
			scheduledBackupName, err := env.GetResourceNameFromYAML(scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return testUtils.CountFilesOnMinio(namespace, minioClientName, "data.tar")
			}, 30).Should(BeNumerically("==", 2))
		})

		It("backs up and restore a cluster with PITR MinIO", func() {
			restoredClusterName := "restore-cluster-pitr-minio"

			prepareClusterForPITROnMinio(namespace, clusterName, backupFile, 2, currentTimestamp)

			err := testUtils.CreateClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, *currentTimestamp, env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, restoredClusterName, tableName, "00000003")
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
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})
	})

	Context("using azure blobs as object storage with storage account access authentication", Ordered, func() {
		// We must be careful here. All the clusters use the same remote storage
		// and that means that we must use different cluster names otherwise
		// we risk mixing WALs and backups
		const azureBlobSampleFile = fixturesDir + "/backup/azure_blob/cluster-with-backup-azure-blob.yaml"
		const clusterRestoreSampleFile = fixturesDir + "/backup/azure_blob/cluster-from-restore.yaml"
		const scheduledBackupSampleFile = fixturesDir +
			"/backup/scheduled_backup_immediate/scheduled-backup-immediate-azure-blob.yaml"
		backupFile := fixturesDir + "/backup/azure_blob/backup-azure-blob.yaml"
		BeforeAll(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if !isAKS {
				Skip("This test is only executed on AKS clusters")
			}
			azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
			azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
			namespace = "cluster-backup-azure-blob"
			clusterName, err = env.GetResourceNameFromYAML(azureBlobSampleFile)
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
		})

		AfterAll(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		// We backup and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)
			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)
			By("uploading a backup", func() {
				// We create a backup
				testUtils.ExecuteBackup(namespace, backupFile, env)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
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

			prepareClusterForPITROnAzureBlob(namespace, clusterName, backupFile,
				azStorageAccount, azStorageKey, 2, currentTimestamp)

			AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

			err := testUtils.CreateClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, *currentTimestamp, env)
			Expect(err).ToNot(HaveOccurred())

			// Restore backup in a new cluster
			AssertClusterRestorePITR(namespace, restoredClusterName, tableName, "00000002")
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
			clusterRestoreSampleFile  = fixturesDir + "/backup/azurite/cluster-from-restore.yaml"
			scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-azurite.yaml"
			scheduledBackupImmediateSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-azurite.yaml"
			backupFile        = fixturesDir + "/backup/azurite/backup.yaml"
			azuriteCaSecName  = "azurite-ca-secret"
			azuriteTLSSecName = "azurite-tls-secret"
		)

		BeforeAll(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if isAKS {
				Skip("This test is only executed on gke, openshift and local")
			}
			namespace = "cluster-backup-azurite"
			clusterName, err = env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating ca and tls certificate secrets", func() {
				// create CA certificates
				_, caPair := testUtils.CreateSecretCA(namespace, clusterName, azuriteCaSecName, true, env)

				// sign and create secret using CA certificate and key
				serverPair, err := caPair.CreateAndSignPair("azurite", certs.CertTypeServer,
					[]string{"azurite.internal.mydomain.net, azurite.default.svc, azurite.default,"},
				)
				Expect(err).ToNot(HaveOccurred())
				serverSecret := serverPair.GenerateCertificateSecret(namespace, azuriteTLSSecName)
				err = env.Client.Create(env.Ctx, serverSecret)
				Expect(err).ToNot(HaveOccurred())
			})

			// Setup Azurite and az cli along with Postgresql cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFile, tableName)
		})

		AfterAll(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("restores a backed up cluster", func() {
			// Restore backup in a new cluster
			AssertClusterRestore(namespace, clusterRestoreSampleFile, tableName)
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
			restoredClusterName := "restore-cluster-pitr-azurite"

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFile, currentTimestamp)

			err := testUtils.CreateClusterFromBackupUsingPITR(namespace, restoredClusterName, backupFile, *currentTimestamp, env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, restoredClusterName, tableName, "00000002")
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
		azuriteBlobSampleFile           = fixturesDir + "/backup/azurite/cluster-backup.yaml"
		externalClusterFileMinio        = fixturesBackupDir + "external-clusters-minio-03.yaml"
		externalClusterFileMinioReplica = fixturesBackupDir + "external-clusters-minio-replica-04.yaml"
		sourceBackupFileMinio           = fixturesBackupDir + "backup-minio-02.yaml"
		clusterSourceFileMinio          = fixturesBackupDir + "source-cluster-minio-01.yaml"
		sourceBackupFileAzure           = fixturesBackupDir + "backup-azure-blob-02.yaml"
		clusterSourceFileAzure          = fixturesBackupDir + "source-cluster-azure-blob-01.yaml"
		externalClusterFileAzure        = fixturesBackupDir + "external-clusters-azure-blob-03.yaml"
		sourceBackupFileAzurePITR       = fixturesBackupDir + "backup-azure-blob-pitr.yaml"
		externalClusterFileAzurite      = fixturesBackupDir + "external-clusters-azurite-03.yaml"
		backupFileAzurite               = fixturesBackupDir + "backup-azurite-02.yaml"
		tableName                       = "to_restore"
		clusterSourceFileAzureSAS       = fixturesBackupDir + "cluster-with-backup-azure-blob-sas.yaml"
		clusterRestoreFileAzureSAS      = fixturesBackupDir + "cluster-from-restore-sas.yaml"
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
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	// Restore cluster using a recovery object store, that is a backup of another cluster,
	// created by Barman Cloud, and defined via the barmanObjectStore option in the externalClusters section
	Context("using minio as object storage", Ordered, func() {
		BeforeAll(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if isAKS {
				Skip("This test is only executed on gke, openshift and local")
			}
			namespace = "recovery-barman-object-minio"
			clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())
			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			By("creating ca and tls certificate secrets", func() {
				// create CA certificate
				_, caPair := testUtils.CreateSecretCA(namespace, clusterName, minioCaSecName, true, env)

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
				err = testUtils.InstallMinio(env, setup, 300)
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

		AfterAll(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})
		It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			externalClusterName, err := env.GetResourceNameFromYAML(externalClusterFileMinio)
			Expect(err).ToNot(HaveOccurred())

			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName)

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceBackupFileMinio, env)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, "data.tar")
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
			externalClusterRestoreName := "restore-external-cluster-pitr"

			prepareClusterForPITROnMinio(namespace, clusterName, sourceBackupFileMinio, 1, currentTimestamp)

			err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnMinio(
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)

			Expect(err).NotTo(HaveOccurred())
			AssertClusterRestorePITR(namespace, externalClusterRestoreName, tableName, "00000002")
		})
		It("restore cluster from barman object using replica option in spec", func() {
			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, "for_restore_repl")

			AssertArchiveWalOnMinio(namespace, clusterName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				testUtils.ExecuteBackup(namespace, sourceBackupFileMinio, env)
				Eventually(func() (int, error) {
					return testUtils.CountFilesOnMinio(namespace, minioClientName, "data.tar")
				}, 30).Should(BeEquivalentTo(1))
			})

			// Replicating a cluster with asynchronous replication
			AssertClusterAsyncReplica(namespace, clusterSourceFileMinio, externalClusterFileMinioReplica, "for_restore_repl")
		})
	})

	Context("using azure blobs as object storage", func() {
		Context("storage account access authentication", Ordered, func() {
			BeforeAll(func() {
				isAKS, err := env.IsAKS()
				Expect(err).ToNot(HaveOccurred())
				if !isAKS {
					Skip("This test is only executed on AKS clusters")
				}
				azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
				azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
				namespace = "recovery-barman-object-azure"
				clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileAzure)
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
			})
			AfterAll(func() {
				err := env.DeleteNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())
			})
			It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				// Write a table and some data on the "app" database
				AssertCreateTestData(namespace, clusterName, tableName)
				AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// Create the backup
					testUtils.ExecuteBackup(namespace, sourceBackupFileAzure, env)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestore(namespace, externalClusterFileAzure, tableName)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				externalClusterName := "external-cluster-azure-pitr"

				prepareClusterForPITROnAzureBlob(namespace, clusterName,
					sourceBackupFileAzurePITR, azStorageAccount, azStorageKey, 1, currentTimestamp)

				err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzure(namespace, externalClusterName,
					clusterName, *currentTimestamp, "backup-storage-creds", azStorageAccount, env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestorePITR(namespace, externalClusterName, tableName, "00000002")
			})
		})

		Context("storage account SAS Token authentication", Ordered, func() {
			BeforeAll(func() {
				isAKS, err := env.IsAKS()
				Expect(err).ToNot(HaveOccurred())
				if !isAKS {
					Skip("This test is only executed on AKS clusters")
				}
				azStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
				azStorageKey = os.Getenv("AZURE_STORAGE_KEY")
				namespace = "cluster-backup-azure-blob-sas"
				clusterName, err = env.GetResourceNameFromYAML(clusterSourceFileAzureSAS)
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
			})

			AfterAll(func() {
				err := env.DeleteNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())
			})
			It("restores cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
				// Write a table and some data on the "app" database
				AssertCreateTestData(namespace, clusterName, tableName)

				// Create a WAL on the primary and check if it arrives on
				// Azure Blob Storage within a short time
				AssertArchiveWalOnAzureBlob(namespace, clusterName, azStorageAccount, azStorageKey)

				By("backing up a cluster and verifying it exists on azure blob storage", func() {
					// We create a Backup
					testUtils.ExecuteBackup(namespace, sourceBackupFileAzureSAS, env)
					// Verifying file called data.tar should be available on Azure blob storage
					Eventually(func() (int, error) {
						return testUtils.CountFilesOnAzureBlobStorage(azStorageAccount, azStorageKey, clusterName, "data.tar")
					}, 30).Should(BeNumerically(">=", 1))
				})

				// Restore backup in a new cluster
				AssertClusterRestore(namespace, clusterRestoreFileAzureSAS, tableName)
			})

			It("restores a cluster with 'PITR' from barman object using "+
				"'barmanObjectStore' option in 'externalClusters' section", func() {
				externalClusterName := "external-cluster-azure-pitr"

				prepareClusterForPITROnAzureBlob(namespace, clusterName,
					sourceBackupFileAzurePITRSAS, azStorageAccount, azStorageKey, 1, currentTimestamp)

				err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzure(namespace, externalClusterName,
					clusterName, *currentTimestamp, "backup-storage-creds-sas", azStorageAccount, env)
				Expect(err).ToNot(HaveOccurred())

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				AssertClusterRestorePITR(namespace, externalClusterName, tableName, "00000002")
			})
		})
	})

	Context("using Azurite blobs as object storage", Ordered, func() {
		BeforeAll(func() {
			isAKS, err := env.IsAKS()
			Expect(err).ToNot(HaveOccurred())
			if isAKS {
				Skip("Test is not run on AKS.")
			}
			namespace = "recovery-barman-object-azurite"
			clusterName, err = env.GetResourceNameFromYAML(azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			err = env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating ca and tls certificate secrets", func() {
				// create CA certificates
				_, caPair := testUtils.CreateSecretCA(namespace, clusterName, azuriteCaSecName, true, env)

				// sign and create secret using CA certificate and key
				serverPair, err := caPair.CreateAndSignPair("azurite", certs.CertTypeServer,
					[]string{"azurite.internal.mydomain.net, azurite.default.svc, azurite.default,"},
				)
				Expect(err).ToNot(HaveOccurred())
				serverSecret := serverPair.GenerateCertificateSecret(namespace, azuriteTLSSecName)
				err = env.Client.Create(env.Ctx, serverSecret)
				Expect(err).ToNot(HaveOccurred())
			})

			// Setup Azurite and az cli along with PostgreSQL cluster
			prepareClusterBackupOnAzurite(namespace, clusterName, azuriteBlobSampleFile, backupFileAzurite, tableName)
		})
		AfterAll(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})
		It("restore cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section", func() {
			// Restore backup in a new cluster
			AssertClusterRestore(namespace, externalClusterFileAzurite, tableName)
		})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			externalClusterRestoreName := "restore-external-cluster-pitr"

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFileAzurite, currentTimestamp)

			//  Create a cluster from a particular time using external backup.
			err := testUtils.CreateClusterFromExternalClusterBackupWithPITROnAzurite(
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp, env)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterRestorePITR(namespace, externalClusterRestoreName, tableName, "00000002")
		})
	})
})
