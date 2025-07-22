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
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azurite - Backup and restore", Label(tests.LabelBackupRestore), func() {
	const (
		tableName             = "to_restore"
		azuriteBlobSampleFile = fixturesDir + "/backup/azurite/cluster-backup.yaml.template"
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(tests.High) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		if !(IsLocal() || IsGKE() || IsOpenshift()) {
			Skip("This test is only executed on gke, openshift and local")
		}
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
			const namespacePrefix = "cluster-backup-azurite"
			var err error
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// Create and assert ca and tls certificate secrets on Azurite
			By("creating ca and tls certificate secrets", func() {
				err := backups.CreateCertificateSecretsOnAzurite(
					env.Ctx, env.Client,
					namespace, clusterName,
					azuriteCaSecName, azuriteTLSSecName,
				)
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
			scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, scheduledBackupImmediateSampleFile)
			Expect(err).ToNot(HaveOccurred())

			AssertScheduledBackupsImmediate(namespace, scheduledBackupImmediateSampleFile, scheduledBackupName)

			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return backups.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
			}, 30).Should(BeNumerically("==", 2))
		})

		It("backs up and restore a cluster with PITR Azurite", func() {
			const (
				restoredClusterName = "restore-cluster-pitr-azurite"
				backupFilePITR      = fixturesDir + "/backup/azurite/backup-pitr.yaml"
			)
			currentTimestamp := new(string)

			prepareClusterForPITROnAzurite(namespace, clusterName, backupFilePITR, currentTimestamp)

			cluster, err := backups.CreateClusterFromBackupUsingPITR(
				env.Ctx,
				env.Client,
				env.Scheme,
				namespace,
				restoredClusterName,
				backupFilePITR,
				*currentTimestamp,
			)
			Expect(err).NotTo(HaveOccurred())
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
			scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("scheduling backups", func() {
				AssertScheduledBackupsAreScheduled(namespace, scheduledBackupSampleFile, 300)
				Eventually(func() (int, error) {
					return backups.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
				}, 60).Should(BeNumerically(">=", 3))
			})

			AssertSuspendScheduleBackups(namespace, scheduledBackupName)
		})
	})
})

var _ = Describe("Clusters Recovery From Barman Object Store", Label(tests.LabelBackupRestore), func() {
	const (
		fixturesBackupDir          = fixturesDir + "/backup/recovery_external_clusters/"
		azuriteBlobSampleFile      = fixturesDir + "/backup/azurite/cluster-backup.yaml.template"
		backupFileAzurite          = fixturesBackupDir + "backup-azurite-02.yaml"
		externalClusterFileAzurite = fixturesBackupDir + "external-clusters-azurite-03.yaml.template"

		azuriteCaSecName  = "azurite-ca-secret"
		azuriteTLSSecName = "azurite-tls-secret"
		tableName         = "to_restore"
	)
	Context("using Azurite blobs as object storage", Ordered, func() {
		var namespace, clusterName string
		BeforeAll(func() {
			if IsAKS() {
				Skip("This test is not run on AKS")
			}
			const namespacePrefix = "recovery-barman-object-azurite"
			var err error
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, azuriteBlobSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// Create and assert ca and tls certificate secrets on Azurite
			By("creating ca and tls certificate secrets", func() {
				err := backups.CreateCertificateSecretsOnAzurite(
					env.Ctx,
					env.Client,
					namespace,
					clusterName,
					azuriteCaSecName,
					azuriteTLSSecName,
				)
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
			currentTimestamp := new(string)
			prepareClusterForPITROnAzurite(namespace, clusterName, backupFileAzuritePITR, currentTimestamp)

			//  Create a cluster from a particular time using external backup.
			restoredCluster, err := backups.CreateClusterFromExternalClusterBackupWithPITROnAzurite(
				env.Ctx, env.Client,
				namespace, externalClusterRestoreName, clusterName, *currentTimestamp)
			Expect(err).NotTo(HaveOccurred())

			AssertClusterWasRestoredWithPITRAndApplicationDB(
				namespace,
				externalClusterRestoreName,
				tableName,
				"00000002",
			)

			By("delete restored cluster", func() {
				Expect(objects.Delete(env.Ctx, env.Client, restoredCluster)).To(Succeed())
			})
		})
	})
})

func prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile string) {
	By("creating the Azurite storage credentials", func() {
		err := backups.CreateStorageCredentialsOnAzurite(env.Ctx, env.Client, namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	By("setting up Azurite to hold the backups", func() {
		// Deploying azurite for blob storage
		err := backups.InstallAzurite(env.Ctx, env.Client, namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	By("setting up az-cli", func() {
		// This is required as we have a service of Azurite running locally.
		// In order to connect, we need az cli inside the namespace
		err := backups.InstallAzCli(env.Ctx, env.Client, namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	// Creating cluster
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

	AssertArchiveConditionMet(namespace, clusterName, "5m")
}

func prepareClusterBackupOnAzurite(
	namespace,
	clusterName,
	clusterSampleFile,
	backupFile,
	tableName string,
) {
	// Setting up Azurite and az cli along with Postgresql cluster
	prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile)
	// Write a table and some data on the "app" database
	tableLocator := TableLocator{
		Namespace:    namespace,
		ClusterName:  clusterName,
		DatabaseName: postgres.AppDBName,
		TableName:    tableName,
	}
	AssertCreateTestData(env, tableLocator)
	assertArchiveWalOnAzurite(namespace, clusterName)

	By("backing up a cluster and verifying it exists on azurite", func() {
		// We create a Backup
		backups.Execute(
			env.Ctx, env.Client, env.Scheme,
			namespace, backupFile, false,
			testTimeouts[testUtils.BackupIsReady],
		)
		// Verifying file called data.tar should be available on Azurite blob storage
		Eventually(func() (int, error) {
			return backups.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
		}, 30).Should(BeNumerically(">=", 1))
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.FirstRecoverabilityPoint).ToNot(BeEmpty()) //nolint:staticcheck
		}, 30).Should(Succeed())
	})
	backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
}

func prepareClusterForPITROnAzurite(
	namespace,
	clusterName,
	backupSampleFile string,
	currentTimestamp *string,
) {
	By("backing up a cluster and verifying it exists on azurite", func() {
		// We create a Backup
		backups.Execute(
			env.Ctx, env.Client, env.Scheme,
			namespace, backupSampleFile, false,
			testTimeouts[testUtils.BackupIsReady],
		)
		// Verifying file called data.tar should be available on Azurite blob storage
		Eventually(func() (int, error) {
			return backups.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
		}, 30).Should(BeNumerically(">=", 1))
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
		TableName:    "for_restore",
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

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", "for_restore"), func() {
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
			forward.Close()
		}()
		Expect(err).ToNot(HaveOccurred())
		insertRecordIntoTable("for_restore", 3, conn)
	})
	assertArchiveWalOnAzurite(namespace, clusterName)
}

func assertArchiveWalOnAzurite(namespace, clusterName string) {
	// Create a WAL on the primary and check if it arrives at the Azure Blob Storage within a short time
	By("archiving WALs and verifying they exist", func() {
		primary := clusterName + "-1"
		latestWAL := switchWalAndGetLatestArchive(namespace, primary)
		// verifying on blob storage using az
		// Define what file we are looking for in Azurite.
		// Escapes are required since az expects forward slashes to be escaped
		path := fmt.Sprintf("%v\\/wals\\/0000000100000000\\/%v.gz", clusterName, latestWAL)
		// verifying on blob storage using az
		Eventually(func() (int, error) {
			return backups.CountFilesOnAzuriteBlobStorage(namespace, clusterName, path)
		}, 60).Should(BeEquivalentTo(1))
	})
}
