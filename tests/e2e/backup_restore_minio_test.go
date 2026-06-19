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
	"path/filepath"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	metricsasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/metrics"
	minioasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/minio"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	replicationasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	storageutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MinIO - Backup and restore", Label(tests.LabelBackupRestore), func() {
	const (
		tableName                 = "to_restore"
		barmanCloudBackupLogEntry = "Starting barman-cloud-backup"
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(tests.High) {
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
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
			const namespacePrefix = "cluster-backup-minio"
			var err error
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating the credentials for minio", func() {
				_, err = secrets.CreateObjectStorageSecret(
					env.Ctx,
					env.Client,
					namespace,
					"backup-storage-creds",
					"minio",
					"minio123",
				)
				Expect(err).ToNot(HaveOccurred())
			})

			// Create ConfigMap and secrets to verify metrics for target database after backup restore
			metricsasserts.AssertCustomMetricsResourcesExist(env, namespace, customQueriesSampleFile, 1, 1)

			// Create the cluster
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterWithMinioSampleFile)

			By("verify connectivity of barman to minio", func() {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (bool, error) {
					connectionStatus, err := minio.TestBarmanConnectivity(
						namespace, clusterName, primaryPod.Name,
						"minio", "minio123", minioEnv.ServiceName)
					return connectionStatus, err
				}, 60).Should(BeTrue())
			})
		})

		AfterAll(func() {
			// The AfterAll runs even when the BeforeAll skipped before
			// creating the namespace; with no namespace there is nothing to
			// clean up.
			if namespace == "" {
				return
			}
			// While namespace deletion would handle this implicitly, explicit deletion helps:
			// - Identify any deletion issues early and in a more clear way rather than waiting for namespace cleanup
			err := resources.DeleteResourcesFromFile(env, namespace, clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
		})

		// We back up and restore a cluster, and verify some expected data to
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
			restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())
			backupName, err := yaml.GetResourceNameFromYAML(env.Scheme, backupFile)
			Expect(err).ToNot(HaveOccurred())
			// Create required test data
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, clusterName, targetDBOne, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, clusterName, targetDBTwo, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, clusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			tableLocator := pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    tableName,
			}
			pgasserts.AssertCreateTestData(env, tableLocator)

			minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, clusterName, clusterName)
			latestTar := minio.GetFilePath(clusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster and verifying it exists on minio, backup path is %v", latestTar),
				func() {
					backup = backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupFile, false,
						testTimeouts[timeouts.BackupIsReady])
					backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)
					Eventually(func() (int, error) {
						return minio.CountFiles(minioEnv, latestTar)
					}, 60).Should(BeEquivalentTo(1))
					Eventually(func() (string, error) {
						cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						if err != nil {
							return "", err
						}
						return cluster.Status.FirstRecoverabilityPoint, err
					}, 30).ShouldNot(BeEmpty())
					Eventually(func() (string, error) {
						cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						if err != nil {
							return "", err
						}
						return cluster.Status.LastSuccessfulBackup, err
					}, 30).ShouldNot(BeEmpty())
					Eventually(func() (string, error) {
						cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						if err != nil {
							return "", err
						}
						return cluster.Status.LastFailedBackup, err
					}, 30).Should(BeEmpty())
				})

			By("verifying the backup is using the expected barman-cloud-backup options", func() {
				Expect(backup).ToNot(BeNil())
				Expect(backup.Status.InstanceID).ToNot(BeNil())
				logEntries, err := logs.ParseJSONLogs(
					env.Ctx, env.Interface, namespace,
					backup.Status.InstanceID.PodName,
				)
				Expect(err).ToNot(HaveOccurred())
				expectedBaseBackupOptions := []string{
					"--immediate-checkpoint",
					"--min-chunk-size=5MB",
					"--read-timeout=59",
				}
				result, err := logs.CheckOptionsForBarmanCommand(
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
					return minio.CountFiles(minioEnv, latestTar)
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
				backups.Execute(
					env.Ctx, env.Client, env.Scheme,
					namespace, backupFile, false,
					testTimeouts[timeouts.BackupIsReady],
				)
				latestTar = minio.GetFilePath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(2))
			})

			By("verifying the backupName is properly set in the status of the backup", func() {
				backup := &apiv1.Backup{}
				err := env.Client.Get(env.Ctx,
					ctrlclient.ObjectKey{Namespace: namespace, Name: backupName},
					backup)
				Expect(err).ToNot(HaveOccurred())
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				// We know that our current images always contain the latest barman version
				if cluster.ShouldForceLegacyBackup() {
					Expect(backup.Status.BackupName).To(BeEmpty())
				} else {
					Expect(backup.Status.BackupName).To(HavePrefix("backup-"))
				}
			})

			// Restore backup in a new cluster, also cover if no application database is configured
			backupasserts.AssertClusterRestore(env, testTimeouts, namespace, clusterRestoreSampleFile, tableName)

			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, restoredClusterName)
			Expect(err).ToNot(HaveOccurred())
			metricsasserts.AssertMetricsData(env, testTimeouts, namespace, targetDBOne, targetDBTwo, targetDBSecret, cluster)

			By("deleting the first restored cluster and waiting for its resources to be removed", func() {
				firstRestoreClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())

				err = resources.DeleteResourcesFromFile(env, namespace, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())

				// The restored cluster is recreated later with the same name, so wait for
				// the cluster and its PVCs to be fully removed first. Otherwise the
				// recreated cluster could adopt stale PVCs instead of performing a fresh
				// restore from the object store, masking or breaking the assertions below.
				Eventually(func() bool {
					_, err := clusterutils.Get(env.Ctx, env.Client, namespace, firstRestoreClusterName)
					return apierrs.IsNotFound(err)
				}, testTimeouts[timeouts.ClusterIsReady]).Should(BeTrue())

				Eventually(func(g Gomega) {
					pvcList, err := storageutils.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())
					for _, pvc := range pvcList.Items {
						g.Expect(pvc.Labels[utils.ClusterLabelName]).ToNot(Equal(firstRestoreClusterName))
					}
				}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
			})

			previous := 0
			latestGZ := filepath.Join("*", clusterName, "*", "*.history.gz")
			By(fmt.Sprintf("checking the previous number of .history files in minio, history file name is %v",
				latestGZ), func() {
				previous, err = minio.CountFiles(minioEnv, latestGZ)
				Expect(err).ToNot(HaveOccurred())
			})

			By("performing a switchover", func() {
				clusterasserts.AssertSwitchover(env, testTimeouts, namespace, clusterName)
			})

			By("checking the number of .history after switchover", func() {
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestGZ)
				}, 60).Should(BeNumerically(">", previous))
			})

			const postSwitchoverTableName = "to_restore_post_switchover"

			By("inserting data into source cluster after switchover", func() {
				postSwitchoverTableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    postSwitchoverTableName,
				}
				pgasserts.AssertCreateTestData(env, postSwitchoverTableLocator)
				minioasserts.AssertArchiveWalOnMinio(
					env, testTimeouts, minioEnv, namespace, clusterName, clusterName)
			})

			By("restoring cluster again and verifying data written after switchover is present", func() {
				resources.CreateResourceFromFile(env, namespace, clusterRestoreSampleFile)
				restoreClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())
				clusterasserts.AssertClusterIsReady(
					env, namespace, restoreClusterName, testTimeouts[timeouts.ClusterIsReadySlow],
				)

				origTableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  restoreClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertDataExpectedCount(env, origTableLocator, 2)

				postSwitchoverTableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  restoreClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    postSwitchoverTableName,
				}
				pgasserts.AssertDataExpectedCount(env, postSwitchoverTableLocator, 2)
			})

			By("deleting the restored cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, clusterRestoreSampleFile)
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

			targetClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterWithMinioStandbySampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster with custom serverName in the backup spec
			clusterasserts.AssertCreateCluster(
				env,
				testTimeouts,
				namespace,
				targetClusterName,
				clusterWithMinioStandbySampleFile,
			)

			// Create required test data
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBOne, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBTwo, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			tableLocator := pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  targetClusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    tableName,
			}
			pgasserts.AssertCreateTestData(env, tableLocator)

			minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, targetClusterName, targetClusterName)
			latestTar := minio.GetFilePath(targetClusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster from standby and verifying it exists on minio, backup path is %v",
				latestTar), func() {
				backups.Execute(
					env.Ctx, env.Client, env.Scheme,
					namespace, backupStandbyFile, true,
					testTimeouts[timeouts.BackupIsReady],
				)
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, targetClusterName)
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, targetClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			By("deleting the standby cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, clusterWithMinioStandbySampleFile)
				Expect(err).ToNot(HaveOccurred())
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

			targetClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterWithMinioSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster with custom serverName in the backup spec
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, targetClusterName, clusterWithMinioSampleFile)

			// Create required test data
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBOne, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBTwo, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, targetClusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			tableLocator := pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  targetClusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    tableName,
			}
			pgasserts.AssertCreateTestData(env, tableLocator)

			minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, targetClusterName, targetClusterName)
			latestTar := minio.GetFilePath(targetClusterName, "data.tar")

			// There should be a backup resource and
			By(fmt.Sprintf("backing up a cluster from standby (defined in backup file) and verifying it exists on minio,"+
				" backup path is %v", latestTar), func() {
				backups.Execute(
					env.Ctx, env.Client, env.Scheme,
					namespace, backupWithTargetFile, true,
					testTimeouts[timeouts.BackupIsReady],
				)
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, targetClusterName)
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(1))
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, targetClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			By("deleting the cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, clusterWithMinioSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// Test that the restore works if the source cluster has a custom
		// backup.barmanObjectStore.serverName that is different from the cluster name
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

			customClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterWithMinioCustomSampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster with custom serverName in the backup spec
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, customClusterName, clusterWithMinioCustomSampleFile)

			// Create required test data
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, customClusterName, targetDBOne, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, customClusterName, targetDBTwo, testTableName)
			pgasserts.AssertCreationOfTestDataForTargetDB(env, namespace, customClusterName, targetDBSecret, testTableName)

			// Write a table and some data on the "app" database
			tableLocator := pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  customClusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    tableName,
			}
			pgasserts.AssertCreateTestData(env, tableLocator)

			minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, customClusterName, clusterServerName)

			// There should be a backup resource and
			By("backing up a cluster and verifying it exists on minio", func() {
				backups.Execute(
					env.Ctx, env.Client, env.Scheme,
					namespace, backupFileCustom, false,
					testTimeouts[timeouts.BackupIsReady],
				)
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, customClusterName)
				latestBaseTar := minio.GetFilePath(clusterServerName, "data.tar")
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestBaseTar)
				}, 60).Should(BeEquivalentTo(1),
					fmt.Sprintf("verify the number of backup %v is equals to 1", latestBaseTar))
				// this is the second backup we take on the bucket
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, customClusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
			})

			// Restore backup in a new cluster
			backupasserts.AssertClusterRestore(env, testTimeouts, namespace, clusterRestoreSampleFile, tableName)

			By("deleting the primary cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, clusterWithMinioCustomSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})

			By("deleting the restored cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, clusterRestoreSampleFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// Create a scheduled backup with the 'immediate' option enabled. We expect the backup to be available
		It("immediately starts a backup using ScheduledBackups 'immediate' option", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_immediate/scheduled-backup-immediate-minio.yaml"
			scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			backupasserts.AssertScheduledBackupsImmediate(env, namespace, scheduledBackupSampleFile, scheduledBackupName)
			latestBaseTar := minio.GetFilePath(clusterName, "data.tar")
			// AssertScheduledBackupsImmediate creates at least two backups, we should find
			// their base backups
			Eventually(func() (int, error) {
				return minio.CountFiles(minioEnv, latestBaseTar)
			}, 60).Should(BeNumerically(">=", 2),
				fmt.Sprintf("verify the number of backup %v is >= 2", latestBaseTar))
		})

		It("backs up and restore a cluster with PITR MinIO", func() {
			const (
				restoredClusterName = "restore-cluster-pitr-minio"
				backupFilePITR      = fixturesDir + "/backup/minio/backup-minio-pitr.yaml"
			)
			currentTimestamp := new(string)
			prepareClusterForPITROnMinio(
				namespace,
				clusterName,
				backupFilePITR,
				3,
				currentTimestamp,
			)

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
			clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReady])

			// Restore backup in a new cluster, also cover if no application database is configured
			backupasserts.AssertClusterWasRestoredWithPITR(
				env, testTimeouts, namespace, restoredClusterName, tableName, "00000003",
			)

			By("deleting the restored cluster", func() {
				Expect(objects.Delete(env.Ctx, env.Client, cluster)).To(Succeed())
			})
		})

		// We create a cluster and a scheduled backup, then it is patched to suspend its
		// execution. We verify that the number of backups does not increase.
		// We then patch it again back to its initial state and verify that
		// the amount of backups keeps increasing again
		It("verifies that scheduled backups can be suspended", func() {
			const scheduledBackupSampleFile = fixturesDir +
				"/backup/scheduled_backup_suspend/scheduled-backup-suspend-minio.yaml"
			scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, scheduledBackupSampleFile)
			Expect(err).ToNot(HaveOccurred())

			By("scheduling backups", func() {
				backupasserts.AssertScheduledBackupsAreScheduled(env, namespace, scheduledBackupSampleFile, 300)
				latestTar := minio.GetFilePath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestTar)
				}, 60).Should(BeNumerically(">=", 2),
					fmt.Sprintf("verify the number of backup %v is great than 2", latestTar))
			})

			backupasserts.AssertSuspendScheduleBackups(env, namespace, scheduledBackupName)
		})

		It("verify tags in backed files", func() {
			minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, clusterName, clusterName)
			tags, err := minio.GetFileTags(minioEnv, minio.GetFilePath(clusterName, "*1.gz"))
			Expect(err).ToNot(HaveOccurred())
			Expect(tags.Tags).ToNot(BeEmpty())

			currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			oldPrimary := currentPrimary.GetName()
			// Force-delete the primary
			quickDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}
			err = pods.Delete(env.Ctx, env.Client, namespace, currentPrimary.GetName(), quickDelete)
			Expect(err).ToNot(HaveOccurred())

			clusterasserts.AssertNewPrimary(env, namespace, clusterName, oldPrimary)

			tags, err = minio.GetFileTags(minioEnv, minio.GetFilePath(clusterName, "*.history.gz"))
			Expect(err).ToNot(HaveOccurred())
			Expect(tags.Tags).ToNot(BeEmpty())
		})
	})

	Context("timeline divergence protection", Ordered, func() {
		var namespace string

		BeforeAll(func() {
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
			const namespacePrefix = "timeline-divergence"
			var err error

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating the credentials for minio", func() {
				_, err = secrets.CreateObjectStorageSecret(
					env.Ctx,
					env.Client,
					namespace,
					"backup-storage-creds",
					"minio",
					"minio123",
				)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("protects replicas from downloading future timeline history files", func() {
			firstClusterFile := fixturesDir + "/backup/minio/cluster-timeline-divergence-1.yaml.template"
			secondClusterFile := fixturesDir + "/backup/minio/cluster-timeline-divergence-2.yaml.template"
			backupFile := fixturesDir + "/backup/minio/backup-timeline-test.yaml"

			firstClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, firstClusterFile)
			Expect(err).ToNot(HaveOccurred())
			secondClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, secondClusterFile)
			Expect(err).ToNot(HaveOccurred())

			By("creating first cluster with 1 instance", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, firstClusterName, firstClusterFile)
			})

			By("creating backup", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupFile, false,
					testTimeouts[timeouts.BackupIsReady])
			})

			By("creating second cluster from backup", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, secondClusterName, secondClusterFile)
			})

			By("verifying second cluster is on timeline 2", func() {
				Eventually(func() (int, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, secondClusterName)
					return cluster.Status.TimelineID, err
				}, 60).Should(BeEquivalentTo(2))
			})

			By("verifying timeline 2 history file is archived", func() {
				minioasserts.AssertArchiveWalOnMinio(
					env, testTimeouts, minioEnv, namespace, secondClusterName, "shared-timeline-test",
				)
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, minio.GetFilePath("shared-timeline-test", "00000002.history*"))
				}, 60).Should(BeNumerically(">", 0))
			})

			By("scaling first cluster to 2 instances", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, firstClusterName)
					if err != nil {
						return err
					}
					cluster.Spec.Instances = 2
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying new replica is streaming", func() {
				// Critical: This verifies the replica successfully joins despite timeline 2
				// history file existing in the shared archive. If the replica were to download
				// the incompatible timeline 2 history file, PostgreSQL would crash with
				// "requested timeline 2 is not a child of this server's history" and enter
				// a crash-loop, causing this assertion to timeout. The validation logic must
				// reject the future timeline file to allow the replica to join successfully.
				replicationasserts.AssertClusterStandbysAreStreaming(
					env,
					namespace,
					firstClusterName,
					testTimeouts[timeouts.ClusterIsReadyQuick],
				)
			})

			By("deleting the first cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, firstClusterFile)
				Expect(err).ToNot(HaveOccurred())
			})

			By("deleting the second cluster", func() {
				err = resources.DeleteResourcesFromFile(env, namespace, secondClusterFile)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
})

var _ = Describe("MinIO - Clusters Recovery from Barman Object Store", Label(tests.LabelBackupRestore), func() {
	const (
		fixturesBackupDir               = fixturesDir + "/backup/recovery_external_clusters/"
		externalClusterFileMinioReplica = fixturesBackupDir + "external-clusters-minio-replica-04.yaml.template"
		clusterSourceFileMinio          = fixturesBackupDir + "source-cluster-minio-01.yaml.template"
		externalClusterFileMinio        = fixturesBackupDir + "external-clusters-minio-03.yaml.template"
		sourceTakeFirstBackupFileMinio  = fixturesBackupDir + "backup-minio-02.yaml"
		sourceTakeSecondBackupFileMinio = fixturesBackupDir + "backup-minio-03.yaml"
		sourceTakeThirdBackupFileMinio  = fixturesBackupDir + "backup-minio-04.yaml"
		tableName                       = "to_restore"
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(tests.High) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// Restore cluster using a recovery object store, that is a backup of another cluster,
	// created by Barman Cloud, and defined via the barmanObjectStore option in the externalClusters section
	Context("using minio as object storage", Ordered, func() {
		var namespace, clusterName string

		BeforeAll(func() {
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
			const namespacePrefix = "recovery-barman-object-minio"
			var err error
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterSourceFileMinio)
			Expect(err).ToNot(HaveOccurred())
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("creating the credentials for minio", func() {
				_, err = secrets.CreateObjectStorageSecret(
					env.Ctx,
					env.Client,
					namespace,
					"backup-storage-creds",
					"minio",
					"minio123",
				)
				Expect(err).ToNot(HaveOccurred())
			})

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the cluster
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterSourceFileMinio)

			By("verify connectivity of barman to minio", func() {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (bool, error) {
					connectionStatus, err := minio.TestBarmanConnectivity(
						namespace, clusterName, primaryPod.Name,
						"minio", "minio123", minioEnv.ServiceName)
					return connectionStatus, err
				}, 60).Should(BeTrue())
			})
		})

		It("restores a cluster from barman object using 'barmanObjectStore' option in 'externalClusters' section",
			func() {
				externalClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, externalClusterFileMinio)
				Expect(err).ToNot(HaveOccurred())

				// Write a table and some data on the "app" database
				tableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertCreateTestData(env, tableLocator)

				minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, clusterName, clusterName)

				// There should be a backup resource and
				By("backing up a cluster and verifying it exists on minio", func() {
					backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, sourceTakeFirstBackupFileMinio,
						false,
						testTimeouts[timeouts.BackupIsReady])
					backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)

					// TODO: this is to force a CHECKPOINT when we run the backup on standby.
					// This should be better handled inside Execute
					minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, clusterName, clusterName)

					latestTar := minio.GetFilePath(clusterName, "data.tar")
					Eventually(func() (int, error) {
						return minio.CountFiles(minioEnv, latestTar)
					}, 60).Should(BeEquivalentTo(1),
						fmt.Sprintf("verify the number of backup %v is equals to 1", latestTar))
					Eventually(func() (string, error) {
						cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
						if err != nil {
							return "", err
						}
						return cluster.Status.FirstRecoverabilityPoint, err
					}, 30).ShouldNot(BeEmpty())
				})

				// Restoring cluster using a recovery barman object store, which is defined
				// in the externalClusters section
				backupasserts.AssertClusterRestore(env, testTimeouts, namespace, externalClusterFileMinio, tableName)

				// verify test data on restored external cluster
				tableLocator = pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  externalClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertDataExpectedCount(env, tableLocator, 2)

				By("deleting the restored cluster", func() {
					err = resources.DeleteResourcesFromFile(env, namespace, externalClusterFileMinio)
					Expect(err).ToNot(HaveOccurred())
				})
			})

		It("restores a cluster with 'PITR' from barman object using 'barmanObjectStore' "+
			" option in 'externalClusters' section", func() {
			externalClusterRestoreName := "restore-external-cluster-pitr"

			currentTimestamp := new(string)
			// We have already written 2 rows in test table 'to_restore' in above test now we will take current
			// timestamp. It will use to restore cluster from source using PITR
			By("getting currentTimestamp", func() {
				ts, err := postgres.GetCurrentTimestamp(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName,
				)
				*currentTimestamp = ts
				Expect(err).ToNot(HaveOccurred())
			})
			By(fmt.Sprintf("writing 2 more entries in table '%v'", tableName), func() {
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
				// insert 2 more rows entries 3,4 on the "app" database
				pgasserts.InsertRecordIntoTable(tableName, 3, conn)
				pgasserts.InsertRecordIntoTable(tableName, 4, conn)
			})
			By("creating second backup and verifying it exists on minio", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, sourceTakeSecondBackupFileMinio,
					false,
					testTimeouts[timeouts.BackupIsReady])
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)
				latestTar := minio.GetFilePath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestTar)
				}, 60).Should(BeEquivalentTo(2),
					fmt.Sprintf("verify the number of backup %v is equals to 2", latestTar))
			})
			var restoredCluster *apiv1.Cluster
			By("create a cluster from backup with PITR", func() {
				var err error
				restoredCluster, err = backups.CreateClusterFromExternalClusterBackupWithPITROnMinio(
					env.Ctx, env.Client,
					namespace, externalClusterRestoreName, clusterName, *currentTimestamp)
				Expect(err).NotTo(HaveOccurred())
			})
			backupasserts.AssertClusterWasRestoredWithPITRAndApplicationDB(env, testTimeouts,
				namespace,
				externalClusterRestoreName,
				tableName,
				"00000002")

			By("delete restored cluster", func() {
				Expect(objects.Delete(env.Ctx, env.Client, restoredCluster)).To(Succeed())
			})
		})

		It("restore cluster from barman object using replica option in spec", func() {
			// Write a table and some data on the "app" database
			tableLocator := pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "for_restore_repl",
			}
			pgasserts.AssertCreateTestData(env, tableLocator)

			minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, clusterName, clusterName)

			By("backing up a cluster and verifying it exists on minio", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, sourceTakeThirdBackupFileMinio, false,
					testTimeouts[timeouts.BackupIsReady])
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)
				latestTar := minio.GetFilePath(clusterName, "data.tar")
				Eventually(func() (int, error) {
					return minio.CountFiles(minioEnv, latestTar)
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
})

func prepareClusterForPITROnMinio(
	namespace,
	clusterName,
	backupSampleFile string,
	expectedVal int,
	currentTimestamp *string,
) {
	const tableNamePitr = "for_restore"

	By("backing up a cluster and verifying it exists on minio", func() {
		backups.Execute(
			env.Ctx, env.Client, env.Scheme,
			namespace, backupSampleFile, false,
			testTimeouts[timeouts.BackupIsReady],
		)
		latestTar := minio.GetFilePath(clusterName, "data.tar")
		Eventually(func() (int, error) {
			return minio.CountFiles(minioEnv, latestTar)
		}, 60).Should(BeNumerically(">=", expectedVal),
			fmt.Sprintf("verify the number of backups %v is greater than or equal to %v", latestTar,
				expectedVal))
		Eventually(func() (string, error) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})

	// Write a table and insert 2 entries on the "app" database
	tableLocator := pgasserts.TableLocator{
		Namespace:    namespace,
		ClusterName:  clusterName,
		DatabaseName: postgres.AppDBName,
		TableName:    tableNamePitr,
	}
	pgasserts.AssertCreateTestData(env, tableLocator)

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
			forward.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		pgasserts.InsertRecordIntoTable(tableNamePitr, 3, conn)
	})
	minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, clusterName, clusterName)
	backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 300)
	backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)
}

func AssertClusterAsyncReplica(namespace, sourceClusterFile, restoreClusterFile, tableName string) {
	By("Async Replication into external cluster", func() {
		restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
		Expect(err).ToNot(HaveOccurred())
		// Add additional data to the source cluster
		sourceClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sourceClusterFile)
		Expect(err).ToNot(HaveOccurred())
		resources.CreateResourceFromFile(env, namespace, restoreClusterFile)
		// We give more time than the usual 600s, since the recovery is slower
		clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

		// Test data should be present on restored primary
		restoredPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())

		// We need the credentials from the source cluster because the replica cluster
		// doesn't create the credentials on its own namespace
		appUser, appUserPass, err := secrets.GetCredentials(
			env.Ctx,
			env.Client,
			sourceClusterName,
			namespace,
			apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())

		forwardRestored, connRestored, err := postgres.ForwardPSQLConnectionWithCreds(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			restoredClusterName,
			postgres.AppDBName,
			appUser,
			appUserPass,
		)
		defer func() {
			_ = connRestored.Close()
			forwardRestored.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		row := connRestored.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", tableName))
		var countString string
		err = row.Scan(&countString)
		Expect(err).ToNot(HaveOccurred())
		Expect(countString).To(BeEquivalentTo("2"))

		forwardSource, connSource, err := postgres.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			sourceClusterName,
			postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = connSource.Close()
			forwardSource.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		// Insert new data in the source cluster
		pgasserts.InsertRecordIntoTable(tableName, 3, connSource)
		minioasserts.AssertArchiveWalOnMinio(env, testTimeouts, minioEnv, namespace, sourceClusterName, sourceClusterName)
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  sourceClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 3)

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())
		expectedReplicas := cluster.Spec.Instances - 1
		// Cascading replicas should be attached to primary replica
		connectedReplicas, err := postgres.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			restoredPrimary, RetryTimeout,
		)
		Expect(connectedReplicas, err).To(BeEquivalentTo(expectedReplicas))
	})
}
