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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure - Backup and restore", Label(tests.LabelBackupRestore), func() {
	const (
		tableName = "to_restore"
	)
	AzureConfiguration := backups.NewAzureConfigurationFromEnv()

	BeforeEach(func() {
		if testLevelEnv.Depth < int(tests.High) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !IsAKS() {
			Skip("This test is only run on AKS clusters")
		}
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
			const namespacePrefix = "cluster-backup-azure-blob"
			var err error
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, azureBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// The Azure Blob Storage should have been created ad-hoc for the tests.
			// The credentials are retrieved from the environment variables, as we can't create
			// a fixture for them
			By("creating the Azure Blob Storage credentials", func() {
				_, err = secrets.CreateObjectStorageSecret(
					env.Ctx,
					env.Client,
					namespace,
					"backup-storage-creds",
					AzureConfiguration.StorageAccount,
					AzureConfiguration.StorageKey,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the cluster
			AssertCreateCluster(namespace, clusterName, azureBlobSampleFile, env)
		})

		// We back up and restore a cluster, and verify some expected data to
		// be there
		It("backs up and restore a cluster", func() {
			// Write a table and some data on the "app" database
			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    tableName,
			}
			AssertCreateTestData(env, tableLocator)
			assertArchiveWalOnAzureBlob(namespace, clusterName, AzureConfiguration)
			By("uploading a backup", func() {
				// We create a backup
				backups.Execute(
					env.Ctx, env.Client, env.Scheme,
					namespace, backupFile, false,
					testTimeouts[testUtils.BackupIsReady],
				)
				backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)

				// Verifying file called data.tar should be available on Azure blob storage
				Eventually(func() (int, error) {
					return backups.CountFilesOnAzureBlobStorage(AzureConfiguration, clusterName, "data.tar")
				}, 30).Should(BeNumerically(">=", 1))
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err //nolint:staticcheck
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
			scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertScheduledBackupsImmediate(namespace, scheduledBackupSampleFile, scheduledBackupName)

			// Only one data.tar files should be present
			Eventually(func() (int, error) {
				return backups.CountFilesOnAzureBlobStorage(AzureConfiguration,
					clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 2))
		})

		It("backs up and restore a cluster with PITR", func() {
			restoredClusterName := "restore-cluster-azure-pitr"
			currentTimestamp := new(string)

			prepareClusterForPITROnAzureBlob(
				namespace,
				clusterName,
				backupFile,
				AzureConfiguration,
				2,
				currentTimestamp,
			)

			assertArchiveWalOnAzureBlob(namespace, clusterName, AzureConfiguration)

			cluster, err := backups.CreateClusterFromBackupUsingPITR(
				env.Ctx,
				env.Client,
				env.Scheme,
				namespace,
				restoredClusterName,
				backupFile,
				*currentTimestamp,
			)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testUtils.ClusterIsReady], env)

			// Restore backup in a new cluster, also cover if no application database is configured
			AssertClusterWasRestoredWithPITR(namespace, restoredClusterName, tableName, "00000002")
			By("deleting the restored cluster", func() {
				Expect(objects.Delete(env.Ctx, env.Client, cluster)).To(Succeed())
			})
		})

		// We create a cluster, create a scheduled backup, patch it to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-azure-blob.yaml"
			scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 480)

				// AssertScheduledBackupsImmediate creates at least two backups, we should find
				// their base backups
				Eventually(func() (int, error) {
					return backups.CountFilesOnAzureBlobStorage(AzureConfiguration,
						clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 2))
			})
			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})
	})
})

var _ = Describe("Azure - Clusters Recovery From Barman Object Store", Label(tests.LabelBackupRestore), func() {
	const (
		fixturesBackupDir            = fixturesDir + "/backup/recovery_external_clusters/"
		sourceBackupFileAzure        = fixturesBackupDir + "backup-azure-blob-02.yaml"
		clusterSourceFileAzure       = fixturesBackupDir + "source-cluster-azure-blob-01.yaml.template"
		externalClusterFileAzure     = fixturesBackupDir + "external-clusters-azure-blob-03.yaml.template"
		sourceBackupFileAzurePITR    = fixturesBackupDir + "backup-azure-blob-pitr.yaml"
		tableName                    = "to_restore"
		clusterSourceFileAzureSAS    = fixturesBackupDir + "cluster-with-backup-azure-blob-sas.yaml.template"
		clusterRestoreFileAzureSAS   = fixturesBackupDir + "cluster-from-restore-sas.yaml.template"
		sourceBackupFileAzureSAS     = fixturesBackupDir + "backup-azure-blob-sas.yaml"
		sourceBackupFileAzurePITRSAS = fixturesBackupDir + "backup-azure-blob-pitr-sas.yaml"
		level                        = tests.High
	)

	currentTimestamp := new(string)
	AzureConfiguration := backups.NewAzureConfigurationFromEnv()

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !IsAKS() {
			Skip("This test is only executed on AKS clusters")
		}
	})

	// Restore cluster using a recovery object store, that is a backup of another cluster,
	// created by Barman Cloud, and defined via the barmanObjectStore option in the externalClusters section

	Context("using azure blobs as object storage", func() {
		Context("storage account access authentication", Ordered, func() {
			var namespace, clusterName string
			BeforeAll(func() {
				const namespacePrefix = "recovery-barman-object-azure"
				var err error
				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterSourceFileAzure)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// The Azure Blob Storage should have been created ad-hoc for the tests.
				// The credentials are retrieved from the environment variables, as we can't create
				// a fixture for them
				By("creating the Azure Blob Storage credentials", func() {
					_, err = secrets.CreateObjectStorageSecret(
						env.Ctx,
						env.Client,
						namespace,
						"backup-storage-creds",
						AzureConfiguration.StorageAccount,
						AzureConfiguration.StorageKey,
					)
					Expect(err).ToNot(HaveOccurred())
				})

				// Create the cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzure, env)
			})

			It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section",
				func() {
					// Write a table and some data on the "app" database
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertCreateTestData(env, tableLocator)
					assertArchiveWalOnAzureBlob(namespace, clusterName, AzureConfiguration)

					By("backing up a cluster and verifying it exists on azure blob storage", func() {
						// Create the backup
						backups.Execute(
							env.Ctx, env.Client, env.Scheme,
							namespace, sourceBackupFileAzure, false,
							testTimeouts[testUtils.BackupIsReady],
						)
						backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
						// Verifying file called data.tar should be available on Azure blob storage
						Eventually(func() (int, error) {
							return backups.CountFilesOnAzureBlobStorage(AzureConfiguration, clusterName, "data.tar")
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
					AzureConfiguration,
					2,
					currentTimestamp,
				)

				restoredCluster, err := backups.CreateClusterFromExternalClusterBackupWithPITROnAzure(
					env.Ctx,
					env.Client,
					namespace,
					externalClusterName,
					clusterName,
					*currentTimestamp,
					"backup-storage-creds",
					AzureConfiguration.StorageAccount,
					AzureConfiguration.BlobContainer,
				)
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
					Expect(objects.Delete(env.Ctx, env.Client, restoredCluster)).To(Succeed())
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
				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterSourceFileAzureSAS)
				Expect(err).ToNot(HaveOccurred())

				// Create a cluster in a namespace we'll delete after the test
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// The Azure Blob Storage should have been created ad-hoc for the tests,
				// we get the credentials from the environment variables as we can't create
				// a fixture for them
				By("creating the Azure Blob Container SAS Token credentials", func() {
					err = backups.CreateSASTokenCredentials(
						env.Ctx,
						env.Client,
						namespace,
						AzureConfiguration.StorageAccount,
						AzureConfiguration.StorageKey,
					)
					Expect(err).ToNot(HaveOccurred())
				})

				// Create the Cluster
				AssertCreateCluster(namespace, clusterName, clusterSourceFileAzureSAS, env)
			})

			It("restores cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section",
				func() {
					// Write a table and some data on the "app" database
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterName,
						DatabaseName: postgres.AppDBName,
						TableName:    tableName,
					}
					AssertCreateTestData(env, tableLocator)

					// Create a WAL on the primary and check if it arrives in the
					// Azure Blob Storage within a short time
					assertArchiveWalOnAzureBlob(namespace, clusterName, AzureConfiguration)

					By("backing up a cluster and verifying it exists on azure blob storage", func() {
						// We create a Backup
						backups.Execute(
							env.Ctx, env.Client, env.Scheme,
							namespace, sourceBackupFileAzureSAS, false,
							testTimeouts[testUtils.BackupIsReady],
						)
						backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
						// Verifying file called data.tar should be available on Azure blob storage
						Eventually(func() (int, error) {
							return backups.CountFilesOnAzureBlobStorage(AzureConfiguration, clusterName, "data.tar")
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
					AzureConfiguration,
					2,
					currentTimestamp,
				)

				restoredCluster, err := backups.CreateClusterFromExternalClusterBackupWithPITROnAzure(
					env.Ctx,
					env.Client,
					namespace,
					externalClusterName,
					clusterName,
					*currentTimestamp,
					"backup-storage-creds-sas",
					AzureConfiguration.StorageAccount,
					AzureConfiguration.BlobContainer,
				)
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
					Expect(objects.Delete(env.Ctx, env.Client, restoredCluster)).To(Succeed())
				})
			})
		})
	})
})

func assertArchiveWalOnAzureBlob(namespace, clusterName string, configuration backups.AzureConfiguration) {
	// Create a WAL on the primary and check if it arrives at the Azure Blob Storage, within a short time
	By("archiving WALs and verifying they exist", func() {
		primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		latestWAL := switchWalAndGetLatestArchive(primary.Namespace, primary.Name)
		// Define what file we are looking for in Azure.
		// Escapes are required since az expects forward slashes to be escaped
		path := fmt.Sprintf("wals\\/0000000100000000\\/%v.gz", latestWAL)
		// Verifying on blob storage using az
		Eventually(func() (int, error) {
			return backups.CountFilesOnAzureBlobStorage(configuration, clusterName, path)
		}, 60).Should(BeEquivalentTo(1))
	})
}

func prepareClusterForPITROnAzureBlob(
	namespace string,
	clusterName string,
	backupSampleFile string,
	azureConfig backups.AzureConfiguration,
	expectedVal int,
	currentTimestamp *string,
) {
	const tableNamePitr = "for_restore"
	By("backing up a cluster and verifying it exists on Azure Blob", func() {
		backups.Execute(
			env.Ctx, env.Client, env.Scheme,
			namespace, backupSampleFile, false,
			testTimeouts[testUtils.BackupIsReady],
		)

		Eventually(func() (int, error) {
			return backups.CountFilesOnAzureBlobStorage(azureConfig, clusterName, "data.tar")
		}, 30).Should(BeEquivalentTo(expectedVal))
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.FirstRecoverabilityPoint).ToNot(BeEmpty()) //nolint:staticcheck
		}, 30).Should(Succeed())
	})

	// Write a table and insert 2 entries on the "app" database
	tableLocator := TableLocator{
		Namespace:    namespace,
		ClusterName:  clusterName,
		DatabaseName: postgres.AppDBName,
		TableName:    tableNamePitr,
	}
	AssertCreateTestData(env, tableLocator)

	By("getting currentTimestamp", func() {
		ts, err := postgres.GetCurrentTimestamp(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName,
		)
		*currentTimestamp = ts
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableNamePitr), func() {
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
		insertRecordIntoTable(tableNamePitr, 3, conn)
	})
	assertArchiveWalOnAzureBlob(namespace, clusterName, azureConfig)
	AssertArchiveConditionMet(namespace, clusterName, "5m")
	backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
}
