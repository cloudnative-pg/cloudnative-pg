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
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/fencing"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tablespaces tests", Label(tests.LabelTablespaces,
	tests.LabelSmoke,
	tests.LabelStorage,
	tests.LabelBasic,
	tests.LabelSnapshot,
	tests.LabelBackupRestore), func() {
	const (
		level           = tests.Medium
		namespacePrefix = "tablespaces"
	)
	var (
		clusterName string
		cluster     *apiv1.Cluster
	)

	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	clusterSetup := func(namespace, clusterManifest string) {
		var err error

		clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster and having it be ready", func() {
			AssertCreateCluster(namespace, clusterName, clusterManifest, env)
		})
		cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
	}

	// Verify that the tablespace exists on the primary pod of a cluster
	hasTablespaceAndOwner := func(cluster *apiv1.Cluster, tablespace, owner string) (bool, error) {
		namespace := cluster.Namespace
		clusterName := cluster.Name
		primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		if err != nil {
			return false, err
		}
		result, stdErr, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: namespace,
				PodName:   primaryPod.Name,
			}, postgres.AppDBName,
			fmt.Sprintf("SELECT 1 FROM pg_catalog.pg_tablespace WHERE spcname = '%s' "+
				"AND pg_catalog.pg_get_userbyid(spcowner) = '%s'",
				tablespace,
				owner),
		)
		if err != nil {
			return false, err
		}
		if stdErr != "" {
			return false, fmt.Errorf("error while checking tablespaces: %s", stdErr)
		}
		return result == "1\n", nil
	}

	Context("on a new cluster with tablespaces", Ordered, func() {
		var namespace, backupName string
		var err error
		const (
			clusterManifest = fixturesDir +
				"/tablespaces/cluster-with-tablespaces.yaml.template"
			clusterBackupManifest = fixturesDir +
				"/tablespaces/cluster-with-tablespaces-backup.yaml.template"
			fullBackupName = "full-barman-backup"
		)
		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// We create the MinIO credentials required to login into the system
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

			clusterSetup(namespace, clusterManifest)
		})

		It("can verify tablespaces and PVC were created", func() {
			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.Short])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.Short])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.Short])
			AssertRoleReconciled(namespace, clusterName, "dante", testTimeouts[timeouts.Short])
			AssertRoleReconciled(namespace, clusterName, "alpha", testTimeouts[timeouts.Short])
			Expect(hasTablespaceAndOwner(cluster, "atablespace", "app")).To(BeTrue())
			Expect(hasTablespaceAndOwner(cluster, "anothertablespace", "dante")).To(BeTrue())
		})

		It("can update the cluster by change the owner of tablespace", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			updateTablespaceOwner(cluster, "anothertablespace", "alpha")

			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (bool, error) {
				return hasTablespaceAndOwner(cluster, "anothertablespace", "alpha")
			}).WithTimeout(30 * time.Second).Should(BeTrue())
		})

		It("can update the cluster to set a tablespace as temporary", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("setting the first tablespace as temporary", func() {
				Expect(cluster.Spec.Tablespaces[0].Temporary).To(BeFalse())
				updatedCluster := cluster.DeepCopy()
				updatedCluster.Spec.Tablespaces[0].Temporary = true
				err = env.Client.Patch(env.Ctx, updatedCluster, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())

				cluster = updatedCluster
			})

			By("checking the temp_tablespaces setting reflects the specification", func() {
				AssertTempTablespaceContent(cluster, 60, cluster.Spec.Tablespaces[0].Name)
			})

			By("creating a temporary table and verifying that it is stored in the temporary tablespace", func() {
				AssertTempTablespaceBehavior(cluster, cluster.Spec.Tablespaces[0].Name)
			})
		})

		It("can create the backup and verify content in the object store", func() {
			backupName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterBackupManifest)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("creating backup %s and verifying backup is ready", backupName), func() {
				backups.Execute(
					env.Ctx, env.Client, env.Scheme,
					namespace, clusterBackupManifest, false,
					testTimeouts[timeouts.BackupIsReady],
				)
				backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
			})

			By("verifying the number of tars in minio", func() {
				latestBaseBackupContainsExpectedTars(clusterName, 1, 3)
			})

			By("verifying backup status", func() {
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.FirstRecoverabilityPoint, err //nolint:staticcheck
				}, 30).ShouldNot(BeEmpty())
			})
		})

		It("can update the cluster adding a new tablespace and backup again", func() {
			By("adding a new tablespace to the cluster", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				addTablespaces(cluster, []apiv1.TablespaceConfiguration{
					{
						Name: "thirdtablespace",
						Owner: apiv1.DatabaseRoleRef{
							Name: "dante",
						},
						Storage: apiv1.StorageConfiguration{
							Size:         "1Gi",
							StorageClass: &storageClassName,
						},
					},
				})

				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())
			})

			By("verifying there are 3 tablespaces and PVCs were created", func() {
				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.Tablespaces).To(HaveLen(3))

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 3, testTimeouts[timeouts.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.PodRollout])
				Expect(hasTablespaceAndOwner(cluster, "atablespace", "app")).To(BeTrue())
				Expect(hasTablespaceAndOwner(cluster, "anothertablespace", "alpha")).To(BeTrue())
				Expect(hasTablespaceAndOwner(cluster, "thirdtablespace", "dante")).To(BeTrue())
			})

			By("waiting for the cluster to be ready", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("verifying expected number of PVCs for tablespaces", func() {
				// 2 pods x 3 tablespaces = 6 pvcs for tablespaces
				eventuallyHasExpectedNumberOfPVCs(6, namespace)
			})

			By("creating a new backup and verifying backup is ready", func() {
				backupCondition, err := backups.GetConditionsInClusterStatus(
					env.Ctx,
					env.Client,
					namespace,
					clusterName,
					apiv1.ConditionBackup,
				)
				Expect(err).ShouldNot(HaveOccurred())
				_, stderr, err := run.Run(
					fmt.Sprintf("kubectl cnpg backup %s -n %s --backup-name %s",
						clusterName, namespace, fullBackupName))
				Expect(stderr).To(BeEmpty())
				Expect(err).ShouldNot(HaveOccurred())
				AssertBackupConditionTimestampChangedInClusterStatus(
					namespace,
					clusterName,
					apiv1.ConditionBackup,
					&backupCondition.LastTransitionTime,
				)

				// TODO: this is to force a CHECKPOINT when we run the backup on standby.
				// This should be better handled inside Execute
				AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

				backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
			})

			By("verifying the number of tars in the latest base backup", func() {
				backups := 2
				eventuallyHasCompletedBackups(namespace, backups)
				// in the latest base backup, we expect 4 tars
				//   (data.tar + 3 tars for each of the 3 tablespaces)
				latestBaseBackupContainsExpectedTars(clusterName, backups, 4)
			})

			By("verifying backup status", func() {
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.FirstRecoverabilityPoint, err //nolint:staticcheck
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.LastSuccessfulBackup, err //nolint:staticcheck
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return "", err
					}
					return cluster.Status.LastFailedBackup, err //nolint:staticcheck
				}, 30).Should(BeEmpty())
			})
		})

		It("can create the cluster by restoring from the object store", func() {
			barmanBackupNameEnv := "BARMAN_BACKUP_NAME"
			err := os.Setenv(barmanBackupNameEnv, fullBackupName)
			Expect(err).ToNot(HaveOccurred())

			const clusterRestoreFromBarmanManifest string = fixturesDir +
				"/tablespaces/restore-cluster-from-barman.yaml.template"

			restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterRestoreFromBarmanManifest)
			Expect(err).ToNot(HaveOccurred())

			By("creating the cluster to be restored through snapshot", func() {
				CreateResourceFromFile(namespace, clusterRestoreFromBarmanManifest)
				// A delay of 5 min when restoring with tablespaces is normal, let's give extra time
				AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow],
					env)
			})

			By("verifying that tablespaces and PVC were created", func() {
				restoredCluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, restoredClusterName)
				Expect(err).ToNot(HaveOccurred())
				AssertClusterHasMountPointsAndVolumesForTablespaces(restoredCluster, 3,
					testTimeouts[timeouts.Short])
				AssertClusterHasPvcsAndDataDirsForTablespaces(restoredCluster, testTimeouts[timeouts.Short])
				AssertDatabaseContainsTablespaces(restoredCluster, testTimeouts[timeouts.Short])
				Expect(hasTablespaceAndOwner(cluster, "atablespace", "app")).To(BeTrue())
				Expect(hasTablespaceAndOwner(cluster, "anothertablespace", "alpha")).To(BeTrue())
				Expect(hasTablespaceAndOwner(cluster, "thirdtablespace", "dante")).To(BeTrue())
			})
		})
	})

	Context("on a new cluster with tablespaces and volumesnapshot support", Ordered, func() {
		var namespace, backupName string
		var err error
		var backupObject *apiv1.Backup
		const (
			clusterManifest = fixturesDir +
				"/tablespaces/cluster-volume-snapshot-tablespaces.yaml.template"
			clusterVolumesnapshoBackupManifest = fixturesDir +
				"/tablespaces/cluster-volume-snapshot-backup.yaml.template"
			clusterVolumesnapshoRestoreManifest = fixturesDir +
				"/tablespaces/cluster-volume-snapshot-tablespaces-restore.yaml.template"
			clusterVolumesnapshoPITRManifest = fixturesDir +
				"/tablespaces/cluster-volume-snapshot-tablespaces-pitr.yaml.template"

			snapshotDataEnv       = "SNAPSHOT_PITR_PGDATA"
			snapshotWalEnv        = "SNAPSHOT_PITR_PGWAL"
			snapshotTbsEnv        = "SNAPSHOT_PITR_PGTABLESPACE"
			recoveryTargetTimeEnv = "SNAPSHOT_PITR"
			tablespace1           = "tbs1"
			table1                = "test_tbs1"
			tablespace2           = "tbs2"
			table2                = "test_tbs2"
		)

		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// We create the required credentials for MinIO
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

			clusterSetup(namespace, clusterManifest)
		})

		It("can verify tablespaces and PVC were created", func() {
			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.Short])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.Short])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.Short])
		})

		It("can create the volume snapshot backup declaratively and verify the backup", func() {
			backupName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterVolumesnapshoBackupManifest)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("creating backup %s and verifying backup is ready", backupName), func() {
				backupObject = backups.Execute(
					env.Ctx,
					env.Client,
					env.Scheme,
					namespace,
					clusterVolumesnapshoBackupManifest,
					false,
					testTimeouts[timeouts.VolumeSnapshotIsReady],
				)
				backups.AssertBackupConditionInClusterStatus(env.Ctx, env.Client, namespace, clusterName)
			})

			By("checking that volumeSnapshots are properly labeled", func() {
				Eventually(func(g Gomega) {
					for _, snapshot := range backupObject.Status.BackupSnapshotStatus.Elements {
						volumeSnapshot, err := backups.GetVolumeSnapshot(env.Ctx, env.Client, namespace, snapshot.Name)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(volumeSnapshot.Name).Should(ContainSubstring(clusterName))
						g.Expect(volumeSnapshot.Labels[utils.BackupNameLabelName]).To(BeEquivalentTo(backupObject.Name))
						g.Expect(volumeSnapshot.Labels[utils.ClusterLabelName]).To(BeEquivalentTo(clusterName))
					}
				}).Should(Succeed())
				Expect(len(backupObject.Status.BackupSnapshotStatus.Elements)).To(BeIdenticalTo(4))
			})
		})

		It("can create the volume snapshot backup using the plugin and verify the backup", func() {
			By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
				// Create a table and insert data 1,2 in each tablespace
				tl1 := TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    table1,
					Tablespace:   tablespace1,
				}
				AssertCreateTestData(env, tl1)
				tl2 := TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    table2,
					Tablespace:   tablespace2,
				}
				AssertCreateTestData(env, tl2)

				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				// Execute a checkpoint
				_, _, err = exec.EventuallyExecQueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: primaryPod.Namespace,
						PodName:   primaryPod.Name,
					}, postgres.PostgresDBName,
					"CHECKPOINT",
					RetryTimeout,
					PollingTime,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			backupName = clusterName + pgTime.GetCurrentTimestampWithFormat("20060102150405")
			By("creating a volumeSnapshot and waiting until it's completed", func() {
				Eventually(func() error {
					return backups.CreateOnDemandBackupViaKubectlPlugin(
						namespace,
						clusterName,
						backupName,
						apiv1.BackupTargetStandby,
						apiv1.BackupMethodVolumeSnapshot,
					)
				}).WithTimeout(time.Minute).WithPolling(5 * time.Second).Should(Succeed())

				// TODO: this is to force a CHECKPOINT when we run the backup on standby.
				// This should probably be moved elsewhere
				AssertArchiveWalOnMinio(namespace, clusterName, clusterName)

				Eventually(func(g Gomega) {
					backupList, err := backups.List(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred())
					for _, backup := range backupList.Items {
						if backup.Name != backupName {
							continue
						}
						backupObject = &backup
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted),
							"Backup should be completed correctly, error message is '%s'",
							backup.Status.Error)
						g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(4))
					}
				}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
			})

			By("checking that volumeSnapshots are properly labeled", func() {
				Eventually(func(g Gomega) {
					for _, snapshot := range backupObject.Status.BackupSnapshotStatus.Elements {
						volumeSnapshot, err := backups.GetVolumeSnapshot(env.Ctx, env.Client, namespace, snapshot.Name)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(volumeSnapshot.Name).Should(ContainSubstring(clusterName))
						g.Expect(volumeSnapshot.Labels[utils.BackupNameLabelName]).To(BeEquivalentTo(backupObject.Name))
						g.Expect(volumeSnapshot.Labels[utils.ClusterLabelName]).To(BeEquivalentTo(clusterName))

					}
				}).Should(Succeed())
			})
		})

		It(fmt.Sprintf("can create the cluster by restoring from the backup %v using volume snapshot", backupName),
			func() {
				err = os.Setenv("BACKUP_NAME", backupName)
				Expect(err).ToNot(HaveOccurred())

				clusterToRestoreName, err := yaml.GetResourceNameFromYAML(env.Scheme,
					clusterVolumesnapshoRestoreManifest)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot", func() {
					CreateResourceFromFile(namespace, clusterVolumesnapshoRestoreManifest)
					AssertClusterIsReady(namespace, clusterToRestoreName, testTimeouts[timeouts.ClusterIsReadySlow],
						env)
				})

				By("verifying that tablespaces and PVC were created", func() {
					restoredCluster, err := clusterutils.Get(env.Ctx, env.Client, namespace,
						clusterToRestoreName)
					Expect(err).ToNot(HaveOccurred())
					AssertClusterHasMountPointsAndVolumesForTablespaces(restoredCluster, 2,
						testTimeouts[timeouts.Short])
					AssertClusterHasPvcsAndDataDirsForTablespaces(restoredCluster, testTimeouts[timeouts.Short])
					AssertDatabaseContainsTablespaces(restoredCluster, testTimeouts[timeouts.Short])
				})

				By("verifying the correct data exists in the restored cluster", func() {
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    table1,
					}
					AssertDataExpectedCount(env, tableLocator, 2)
					tableLocator = TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToRestoreName,
						DatabaseName: postgres.AppDBName,
						TableName:    table2,
					}
					AssertDataExpectedCount(env, tableLocator, 2)
				})
			})

		It(fmt.Sprintf("can create the cluster by recovery from volume snapshot backup with pitr %v", backupName),
			func() {
				By("inserting test data and creating WALs on the cluster to be snapshotted", func() {
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

					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(table1, 3, conn)
					insertRecordIntoTable(table1, 4, conn)

					insertRecordIntoTable(table2, 3, conn)
					insertRecordIntoTable(table2, 4, conn)

					// Because GetCurrentTimestamp() rounds down to the second and is executed
					// right after the creation of the test data, we wait for 1s to avoid not
					// including the newly created data within the recovery_target_time
					time.Sleep(1 * time.Second)
					// Get the recovery_target_time and pass it to the template engine
					recoveryTargetTime, err := postgres.GetCurrentTimestamp(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						namespace, clusterName,
					)
					Expect(err).ToNot(HaveOccurred())
					err = os.Setenv(recoveryTargetTimeEnv, recoveryTargetTime)
					Expect(err).ToNot(HaveOccurred())

					// Insert 2 more rows which we expect not to be present at the end of the recovery
					insertRecordIntoTable(table1, 5, conn)
					insertRecordIntoTable(table1, 6, conn)

					insertRecordIntoTable(table2, 5, conn)
					insertRecordIntoTable(table2, 6, conn)

					// Close and archive the current WAL file
					AssertArchiveWalOnMinio(namespace, clusterName, clusterName)
				})
				By("fetching the volume snapshots", func() {
					snapshotList, err := getSnapshots(backupName, clusterName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backupObject.Status.BackupSnapshotStatus.Elements)))

					envVars := storage.EnvVarsForSnapshots{
						DataSnapshot:             snapshotDataEnv,
						WalSnapshot:              snapshotWalEnv,
						TablespaceSnapshotPrefix: snapshotTbsEnv,
					}
					err = storage.SetSnapshotNameAsEnv(&snapshotList, backupObject, envVars)
					Expect(err).ToNot(HaveOccurred())
				})

				clusterToPITRName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterVolumesnapshoPITRManifest)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster to be restored through snapshot", func() {
					CreateResourceFromFile(namespace, clusterVolumesnapshoPITRManifest)
					AssertClusterIsReady(namespace, clusterToPITRName, testTimeouts[timeouts.ClusterIsReadySlow],
						env)
				})

				By("can verify tablespaces and PVC were created", func() {
					recoveryCluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterToPITRName)
					Expect(err).ToNot(HaveOccurred())
					AssertClusterHasMountPointsAndVolumesForTablespaces(recoveryCluster, 2,
						testTimeouts[timeouts.Short])
					AssertClusterHasPvcsAndDataDirsForTablespaces(recoveryCluster, testTimeouts[timeouts.Short])
					AssertDatabaseContainsTablespaces(recoveryCluster, testTimeouts[timeouts.Short])
				})

				By("verifying the correct data exists in the restored cluster", func() {
					tableLocator := TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToPITRName,
						DatabaseName: postgres.AppDBName,
						TableName:    table1,
					}
					AssertDataExpectedCount(env, tableLocator, 4)
					tableLocator = TableLocator{
						Namespace:    namespace,
						ClusterName:  clusterToPITRName,
						DatabaseName: postgres.AppDBName,
						TableName:    table2,
					}
					AssertDataExpectedCount(env, tableLocator, 4)
				})
			})
	})

	Context("on a plain cluster with primaryUpdateMethod=restart", Ordered, func() {
		var namespace string
		var initialPrimary *corev1.Pod
		clusterManifest := fixturesDir + "/tablespaces/cluster-without-tablespaces.yaml.template"
		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterSetup(namespace, clusterManifest)
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())
			initialPrimary, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can update cluster by adding tablespaces", func() {
			By("adding tablespaces to the spec and patching", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeFalse())

				addTablespaces(cluster, []apiv1.TablespaceConfiguration{
					{
						Name: "atablespace",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
					{
						Name: "anothertablespace",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
				})

				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())
			})
			By("verify tablespaces and PVC were created", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.PodRollout])
			})
			By("waiting for the cluster to be ready again", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("checking the primary didn't switch", func() {
				AssertPrimaryUpdateMethod(namespace, clusterName, initialPrimary, apiv1.PrimaryUpdateMethodRestart)
			})
		})

		It("can hibernate via annotation a cluster with tablespaces", func() {
			assertCanHibernateClusterWithTablespaces(namespace, clusterName, hibernateDeclaratively, 6)
		})

		It("can fence a cluster with tablespaces using the plugin", func() {
			By("verifying expected PVCs for tablespaces before hibernate", func() {
				eventuallyHasExpectedNumberOfPVCs(6, namespace)
			})

			By("fencing the cluster", func() {
				err := fencing.On(env.Ctx, env.Client, "*", namespace, clusterName, fencing.UsingPlugin)
				Expect(err).ToNot(HaveOccurred())
			})

			By("check all instances become not ready", func() {
				Eventually(func() (bool, error) {
					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return false, err
					}
					var hasReadyPod bool
					for _, pod := range podList.Items {
						for _, podInfo := range pod.Status.ContainerStatuses {
							if podInfo.Name == specs.PostgresContainerName {
								if podInfo.Ready {
									hasReadyPod = true
								}
							}
						}
					}
					return hasReadyPod, nil
				}, 120, 5).Should(BeFalse())
			})

			By("un-fencing the cluster", func() {
				err := fencing.Off(env.Ctx, env.Client, "*", namespace, clusterName, fencing.UsingPlugin)
				Expect(err).ToNot(HaveOccurred())
			})

			By("all instances become ready", func() {
				Eventually(func() (bool, error) {
					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return false, err
					}
					var hasReadyPod bool
					for _, pod := range podList.Items {
						for _, podInfo := range pod.Status.ContainerStatuses {
							if podInfo.Name == specs.PostgresContainerName {
								if podInfo.Ready {
									hasReadyPod = true
								}
							}
						}
					}
					return hasReadyPod, nil
				}, 120, 5).Should(BeTrue())
			})

			By("verify tablespaces and PVC are there", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.PodRollout])
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("verifying all PVCs for tablespaces are recreated", func() {
				eventuallyHasExpectedNumberOfPVCs(6, namespace)
			})
		})
	})

	Context("on a plain cluster with primaryUpdateMethod=switchover", Ordered, func() {
		var namespace string
		var initialPrimary *corev1.Pod
		clusterManifest := fixturesDir + "/tablespaces/cluster-without-tablespaces.yaml.template"
		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterSetup(namespace, clusterManifest)
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())
			initialPrimary, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can update cluster adding tablespaces", func() {
			By("patch cluster with primaryUpdateMethod=switchover", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeFalse())

				updated := cluster.DeepCopy()
				updated.Spec.PrimaryUpdateMethod = apiv1.PrimaryUpdateMethodSwitchover
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})
			By("waiting for the cluster to be ready", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})
			By("adding tablespaces to the spec and patching", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeFalse())

				updated := cluster.DeepCopy()
				updated.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
					{
						Name: "atablespace",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
					{
						Name: "anothertablespace",
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())

				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())
			})

			By("verifying tablespaces and PVC were created", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.PodRollout])
			})
			By("waiting for the cluster to be ready again", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("checking the primary did switch", func() {
				AssertPrimaryUpdateMethod(namespace, clusterName, initialPrimary, apiv1.PrimaryUpdateMethodSwitchover)
			})
		})
	})
})

func addTablespaces(cluster *apiv1.Cluster, tbsSlice []apiv1.TablespaceConfiguration) {
	updated := cluster.DeepCopy()
	updated.Spec.Tablespaces = append(updated.Spec.Tablespaces, tbsSlice...)

	err := env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
	Expect(err).ToNot(HaveOccurred())
}

func updateTablespaceOwner(cluster *apiv1.Cluster, tablespaceName, newOwner string) {
	updated := cluster.DeepCopy()
	for idx, value := range updated.Spec.Tablespaces {
		if value.Name == tablespaceName {
			updated.Spec.Tablespaces[idx].Owner.Name = newOwner
		}
	}
	err := env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
	Expect(err).ToNot(HaveOccurred())
}

func AssertRoleReconciled(
	namespace, clusterName,
	roleName string,
	timeout int,
) {
	By(fmt.Sprintf("checking if role %v is in reconciled status", roleName), func() {
		Eventually(func(g Gomega) bool {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			for state, names := range cluster.Status.ManagedRolesStatus.ByStatus {
				if state == apiv1.RoleStatusReconciled {
					return len(names) > 0 && slices.Contains(names, roleName)
				}
			}
			return false
		}, timeout).Should(BeTrue())
	})
}

func AssertClusterHasMountPointsAndVolumesForTablespaces(
	cluster *apiv1.Cluster,
	numTablespaces int,
	timeout int,
) {
	namespace := cluster.Namespace
	clusterName := cluster.Name
	podMountPaths := func(pod corev1.Pod) (bool, []string) {
		var hasPostgresContainer bool
		var mountPaths []string
		for _, ctr := range pod.Spec.Containers {
			if ctr.Name == "postgres" {
				hasPostgresContainer = true
				for _, mt := range ctr.VolumeMounts {
					mountPaths = append(mountPaths, mt.MountPath)
				}
			}
		}
		return hasPostgresContainer, mountPaths
	}

	By("checking the mount points and volumes in the pods", func() {
		Eventually(func(g Gomega) {
			g.Expect(cluster.ContainsTablespaces()).To(BeTrue())
			g.Expect(cluster.Spec.Tablespaces).To(HaveLen(numTablespaces))
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				g.Expect(pod.Spec.Containers).ToNot(BeEmpty())
				hasPostgresContainer, mountPaths := podMountPaths(pod)
				g.Expect(hasPostgresContainer).To(BeTrue())
				for _, tbsConfig := range cluster.Spec.Tablespaces {
					g.Expect(mountPaths).To(ContainElements(
						path.Join("/var/lib/postgresql/tablespaces/", tbsConfig.Name),
					))
				}

				volumeNames := make([]string, 0, len(pod.Spec.Volumes))
				var claimNames []string
				for _, vol := range pod.Spec.Volumes {
					volumeNames = append(volumeNames, vol.Name)
					if vol.PersistentVolumeClaim != nil {
						claimNames = append(claimNames, vol.PersistentVolumeClaim.ClaimName)
					}
				}
				for _, tbsConfig := range cluster.Spec.Tablespaces {
					g.Expect(volumeNames).To(ContainElement(
						specs.VolumeMountNameForTablespace(tbsConfig.Name),
					))
					g.Expect(claimNames).To(ContainElement(
						pod.Name + "-tbs-" + tbsConfig.Name,
					))
				}
			}
		}, timeout).Should(Succeed())
	})
}

func getPostgresContainer(pod corev1.Pod) *corev1.Container {
	for _, cr := range pod.Spec.Containers {
		if cr.Name == specs.PostgresContainerName {
			return &cr
		}
	}
	return nil
}

// if there's a security context with a specific UID to use for the DB, use it,
// otherwise use the default postgres UID
func getDatabasUserUID(cluster *apiv1.Cluster, dbContainer *corev1.Container) int64 {
	if dbContainer.SecurityContext.RunAsUser != nil {
		return *dbContainer.SecurityContext.RunAsUser
	}
	return cluster.GetPostgresUID()
}

func AssertClusterHasPvcsAndDataDirsForTablespaces(cluster *apiv1.Cluster, timeout int) {
	namespace := cluster.Namespace
	clusterName := cluster.Name
	By("checking all the required PVCs were created", func() {
		Eventually(func(g Gomega) {
			pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
			g.Expect(err).ShouldNot(HaveOccurred())
			var tablespacePvcNames []string
			for _, pvc := range pvcList.Items {
				roleLabel := pvc.Labels[utils.PvcRoleLabelName]
				if roleLabel != string(utils.PVCRolePgTablespace) {
					continue
				}
				tablespacePvcNames = append(tablespacePvcNames, pvc.Name)
				tbsName := pvc.Labels[utils.TablespaceNameLabelName]
				g.Expect(tbsName).ToNot(BeEmpty())
				labelTbsInCluster := cluster.GetTablespaceConfiguration(tbsName)
				g.Expect(labelTbsInCluster).ToNot(BeNil())
				for _, tbsConfig := range cluster.Spec.Tablespaces {
					if tbsName == tbsConfig.Name {
						g.Expect(pvc.Spec.Resources.Requests.Storage()).
							To(BeEquivalentTo(tbsConfig.Storage.GetSizeOrNil()))
					}
				}
			}
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				for _, tbsConfig := range cluster.Spec.Tablespaces {
					g.Expect(tablespacePvcNames).To(ContainElement(pod.Name + "-tbs-" + tbsConfig.Name))
				}
			}
		}, timeout).Should(Succeed())
	})
	By("checking the data directory for the tablespaces is owned by postgres", func() {
		Eventually(func(g Gomega) {
			// minio may in the same namespace with cluster pod
			pvcList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ShouldNot(HaveOccurred())
			for _, pod := range pvcList.Items {
				for _, tbsConfig := range cluster.Spec.Tablespaces {
					dataDir := fmt.Sprintf("/var/lib/postgresql/tablespaces/%s/data", tbsConfig.Name)
					owner, stdErr, err := exec.CommandInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: namespace,
							PodName:   pod.Name,
						}, nil,
						"stat", "-c", `'%u'`, dataDir,
					)

					targetContainer := getPostgresContainer(pod)
					g.Expect(targetContainer).NotTo(BeNil())
					dbUser := getDatabasUserUID(cluster, targetContainer)

					g.Expect(stdErr).To(BeEmpty())
					g.Expect(err).ShouldNot(HaveOccurred())
					g.Expect(owner).To(ContainSubstring(strconv.FormatInt(dbUser, 10)))
				}
			}
		}, timeout).Should(Succeed())
	})
}

func AssertDatabaseContainsTablespaces(cluster *apiv1.Cluster, timeout int) {
	namespace := cluster.Namespace
	clusterName := cluster.Name
	By("checking the expected tablespaces are in the database", func() {
		Eventually(func(g Gomega) {
			instances, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ShouldNot(HaveOccurred())
			var tbsListing string
			for _, instance := range instances.Items {
				var stdErr string
				var err error
				tbsListing, stdErr, err = exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   instance.Name,
					}, postgres.AppDBName,
					"SELECT oid, spcname, pg_catalog.pg_get_userbyid(spcowner) FROM pg_catalog.pg_tablespace",
				)
				g.Expect(stdErr).To(BeEmpty())
				g.Expect(err).ShouldNot(HaveOccurred())
				for _, tbsConfig := range cluster.Spec.Tablespaces {
					g.Expect(tbsListing).To(ContainSubstring(tbsConfig.Name))
				}
			}
			GinkgoWriter.Printf("Tablespaces in DB:\n%s\n", tbsListing)
		}, timeout).Should(Succeed())
	})
}

func AssertTempTablespaceContent(cluster *apiv1.Cluster, timeout int, content string) {
	namespace := cluster.Namespace
	clusterName := cluster.Name
	By("checking the expected setting in a new PG session", func() {
		Eventually(func(g Gomega) {
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				g.Expect(err).ShouldNot(HaveOccurred())
			}

			settingValue, stdErr, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   primary.Name,
				}, postgres.AppDBName,
				"SHOW temp_tablespaces",
			)
			g.Expect(stdErr).To(BeEmpty())
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(strings.Trim(settingValue, " \n")).To(Equal(content))
			GinkgoWriter.Printf("temp_tablespaces is currently set to:\n%s\n", settingValue)
		}, timeout).Should(Succeed())
	})
}

func AssertTempTablespaceBehavior(cluster *apiv1.Cluster, expectedTempTablespaceName string) {
	namespace := cluster.Namespace
	clusterName := cluster.Name

	primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	if err != nil {
		Expect(err).ShouldNot(HaveOccurred())
	}

	By("checking the temporary table is created into the temporary tablespace", func() {
		commandOutput, stdErr, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: namespace,
				PodName:   primary.Name,
			}, postgres.AppDBName,
			"CREATE TEMPORARY TABLE cnp_e2e_test_table (i INTEGER); "+
				"SELECT spcname FROM pg_catalog.pg_tablespace WHERE OID="+
				"(SELECT reltablespace FROM pg_catalog.pg_class WHERE oid = 'cnp_e2e_test_table'::regclass)",
		)
		Expect(stdErr).To(BeEmpty())
		Expect(err).ShouldNot(HaveOccurred())
		commandOutputLines := strings.Split(strings.Trim(commandOutput, " \n"), "\n")
		Expect(commandOutputLines[len(commandOutputLines)-1]).To(Equal(expectedTempTablespaceName))
		GinkgoWriter.Printf("CREATE TEMPORARY ... command output was:\n%s\n", commandOutput)
	})
}

func assertCanHibernateClusterWithTablespaces(
	namespace string,
	clusterName string,
	method hibernationMethod,
	keptPVCs int,
) {
	By("verifying expected PVCs for tablespaces before hibernate", func() {
		eventuallyHasExpectedNumberOfPVCs(6, namespace)
	})

	By("hibernate the cluster", func() {
		err := hibernateOn(env.Ctx, env.Client, namespace, clusterName, method)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("verifying cluster %v pods are removed", clusterName), func() {
		Eventually(func(g Gomega) {
			podList, _ := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(podList.Items).Should(BeEmpty())
		}, 300).Should(Succeed())
	})

	By("verifying expected number of PVCs for tablespaces are kept in hibernation", func() {
		eventuallyHasExpectedNumberOfPVCs(keptPVCs, namespace)
	})

	By("hibernate off the cluster", func() {
		err := hibernateOff(env.Ctx, env.Client, namespace, clusterName, method)
		Expect(err).ToNot(HaveOccurred())
	})

	By("waiting for the cluster to be ready", func() {
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
	})

	By("verify tablespaces and PVC are there", func() {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.ContainsTablespaces()).To(BeTrue())

		AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[timeouts.PodRollout])
		AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[timeouts.PodRollout])
		AssertDatabaseContainsTablespaces(cluster, testTimeouts[timeouts.PodRollout])
	})

	By("verifying all PVCs for tablespaces are recreated", func() {
		eventuallyHasExpectedNumberOfPVCs(6, namespace)
	})
}

func eventuallyHasExpectedNumberOfPVCs(pvcCount int, namespace string) {
	By(fmt.Sprintf("checking cluster eventually has %d PVCs for tablespaces", pvcCount))
	Eventually(func(g Gomega) {
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())
		tbsPvc := 0
		for _, pvc := range pvcList.Items {
			roleLabel := pvc.Labels[utils.PvcRoleLabelName]
			if roleLabel != string(utils.PVCRolePgTablespace) {
				continue
			}
			tbsPvc++
		}
		g.Expect(tbsPvc).Should(Equal(pvcCount))
	}, testTimeouts[timeouts.ClusterIsReady]).Should(Succeed())
}

func eventuallyHasCompletedBackups(namespace string, numBackups int) {
	Eventually(func(g Gomega) {
		backups, err := backups.List(env.Ctx, env.Client, namespace)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(backups.Items).To(HaveLen(numBackups))

		completedBackups := 0
		for _, backup := range backups.Items {
			if string(backup.Status.Phase) == "completed" {
				completedBackups++
			}
		}
		g.Expect(completedBackups).To(Equal(numBackups))
	}, 120).Should(Succeed())
}

func latestBaseBackupContainsExpectedTars(
	clusterName string,
	numBackups int,
	expectedTars int,
) {
	Eventually(func(g Gomega) {
		// we list the backup.info files to get the listing of base backups
		// directories in minio
		backupInfoFiles := filepath.Join("*", clusterName, "base", "*", "*.info")
		ls, err := minio.ListFiles(minioEnv, backupInfoFiles)
		g.Expect(err).ShouldNot(HaveOccurred())
		frags := strings.Split(ls, "\n")
		slices.Sort(frags)
		report := fmt.Sprintf("directories:\n%s\n", strings.Join(frags, "\n"))
		g.Expect(frags).To(HaveLen(numBackups), report)
		latestBaseBackup := filepath.Dir(frags[numBackups-1])
		tarsInLastBackup := strings.TrimPrefix(filepath.Join(latestBaseBackup, "*.tar"), "minio/")
		listing, err := minio.ListFiles(minioEnv, tarsInLastBackup)
		g.Expect(err).ShouldNot(HaveOccurred())
		report += fmt.Sprintf("tar listing:\n%s\n", listing)
		numTars, err := minio.CountFiles(minioEnv, tarsInLastBackup)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(numTars).To(Equal(expectedTars), report)
	}, 120).Should(Succeed())
}

func getSnapshots(
	backupName string,
	clusterName string,
	namespace string,
) (volumesnapshotv1.VolumeSnapshotList, error) {
	var snapshotList volumesnapshotv1.VolumeSnapshotList
	err := env.Client.List(env.Ctx, &snapshotList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName:    clusterName,
			utils.BackupNameLabelName: backupName,
		})
	if err != nil {
		return snapshotList, err
	}

	return snapshotList, nil
}

type hibernationMethod string

const (
	// hibernateDeclaratively it is a keyword to use while fencing on/off the instances using annotation method
	hibernateDeclaratively hibernationMethod = "annotation"
)

func hibernateOn(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName string,
	method hibernationMethod,
) error {
	switch method {
	case hibernateDeclaratively:
		cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOn

		err = crudClient.Patch(context.Background(), cluster, client.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}

func hibernateOff(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName string,
	method hibernationMethod,
) error {
	switch method {
	case hibernateDeclaratively:
		cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOff

		err = crudClient.Patch(context.Background(), cluster, client.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}
