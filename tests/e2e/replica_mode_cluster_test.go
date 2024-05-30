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
	"strings"
	"time"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v7/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replica Mode", Label(tests.LabelReplication), func() {
	const (
		replicaModeClusterDir = "/replica_mode_cluster/"
		srcClusterName        = "cluster-replica-src"
		srcClusterSample      = fixturesDir + replicaModeClusterDir + srcClusterName + ".yaml.template"
		level                 = tests.Medium
	)

	// those values are present in the cluster manifests
	const (
		// sourceDBName is the name of the database in the source cluster
		sourceDBName = "appSrc"
		// Application database configuration is skipped for replica clusters,
		// so we expect these to not be present
		replicaDBName = "appTgt"
		replicaUser   = "userTgt"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("can bootstrap a replica cluster using TLS auth", func() {
		It("should work", func() {
			const (
				replicaClusterSampleTLS = fixturesDir + replicaModeClusterDir + "cluster-replica-tls.yaml.template"
				replicaNamespacePrefix  = "replica-mode-tls-auth"
				testTableName           = "replica_mode_tls_auth"
			)

			replicaNamespace, err := env.CreateUniqueNamespace(replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(replicaNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(replicaNamespace)
			})
			AssertCreateCluster(replicaNamespace, srcClusterName, srcClusterSample, env)

			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterSampleTLS,
				testTableName,
				psqlClientPod)
		})
	})

	Context("can bootstrap a replica cluster using basic auth", func() {
		It("can be detached from the source cluster", func() {
			const (
				replicaClusterSampleBasicAuth = fixturesDir + replicaModeClusterDir + "cluster-replica-basicauth.yaml.template"
				replicaNamespacePrefix        = "replica-mode-basic-auth"
				testTableName                 = "replica_mode_basic_auth"
			)

			replicaClusterName, err := env.GetResourceNameFromYAML(replicaClusterSampleBasicAuth)
			Expect(err).ToNot(HaveOccurred())
			replicaNamespace, err := env.CreateUniqueNamespace(replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(replicaNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(replicaNamespace)
			})
			AssertCreateCluster(replicaNamespace, srcClusterName, srcClusterSample, env)

			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterSampleBasicAuth,
				testTableName,
				psqlClientPod)

			AssertDetachReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterName,
				replicaDBName,
				replicaUser,
				"replica_mode_basic_auth_detach")
		})

		It("should be able to switch to replica cluster and sync data", func(ctx SpecContext) {
			const (
				clusterOneName = "cluster-one"
				clusterTwoName = "cluster-two"
				clusterOneFile = fixturesDir + replicaModeClusterDir +
					"cluster-demotion-one.yaml.template"
				clusterTwoFile = fixturesDir + replicaModeClusterDir +
					"cluster-demotion-two.yaml.template"
				testTableName = "replica_promotion_demotion"
			)
			var clusterOnePrimary, clusterTwoPrimary *corev1.Pod

			getReplicaClusterSwitchCondition := func(conditions []metav1.Condition) *metav1.Condition {
				for _, condition := range conditions {
					if condition.Type == replicaclusterswitch.ConditionReplicaClusterSwitch {
						return &condition
					}
				}
				return nil
			}

			namespace, err := env.CreateUniqueNamespace("replica-promotion-demotion")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, clusterOneName, clusterOneFile, env)

			AssertReplicaModeCluster(
				namespace,
				clusterOneName,
				sourceDBName,
				clusterTwoFile,
				testTableName,
				psqlClientPod)

			// turn the src cluster into a replica
			By("setting replica mode on the src cluster", func() {
				cluster, err := env.GetCluster(namespace, clusterOneName)
				Expect(err).ToNot(HaveOccurred())
				cluster.Spec.ReplicaCluster.Enabled = true
				err = env.Client.Update(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
				AssertClusterIsReady(namespace, clusterOneName, testTimeouts[testUtils.ClusterIsReady], env)
				time.Sleep(time.Second * 10)
				Eventually(func(g Gomega) {
					cluster, err := env.GetCluster(namespace, clusterOneName)
					g.Expect(err).ToNot(HaveOccurred())
					condition := getReplicaClusterSwitchCondition(cluster.Status.Conditions)
					g.Expect(condition).ToNot(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				}).Should(Succeed())
			})

			By("checking that src cluster is now a replica cluster", func() {
				Eventually(func() error {
					clusterOnePrimary, err = env.GetClusterPrimary(namespace, clusterOneName)
					return err
				}, 30, 3).Should(BeNil())
				AssertPgRecoveryMode(clusterOnePrimary, true)
			})

			// turn the dst cluster into a primary
			By("disabling the replica mode on the dst cluster", func() {
				cluster, err := env.GetCluster(namespace, clusterTwoName)
				Expect(err).ToNot(HaveOccurred())
				cluster.Spec.ReplicaCluster.Enabled = false
				err = env.Client.Update(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
				AssertClusterIsReady(namespace, clusterTwoName, testTimeouts[testUtils.ClusterIsReady], env)
			})

			By("checking that dst cluster has been promoted", func() {
				Eventually(func() error {
					clusterTwoPrimary, err = env.GetClusterPrimary(namespace, clusterTwoName)
					return err
				}, 30, 3).Should(BeNil())
				AssertPgRecoveryMode(clusterTwoPrimary, false)
			})

			By("creating a new data in the new source cluster", func() {
				AssertCreateTestDataWithDatabaseName(namespace, clusterTwoName, sourceDBName,
					"new_test_table", clusterTwoPrimary)
			})

			By("checking that the data is present in the old src cluster", func() {
				AssertDataExpectedCountWithDatabaseName(
					namespace,
					clusterOnePrimary.Name,
					sourceDBName,
					"new_test_table",
					2,
				)
			})
		})
	})

	Context("archive mode set to 'always' on designated primary", func() {
		It("verifies replica cluster can archive WALs from the designated primary", func() {
			const (
				replicaClusterSample   = fixturesDir + replicaModeClusterDir + "cluster-replica-archive-mode-always.yaml.template"
				replicaNamespacePrefix = "replica-mode-archive"
				testTableName          = "replica_mode_archive"
			)

			replicaClusterName, err := env.GetResourceNameFromYAML(replicaClusterSample)
			Expect(err).ToNot(HaveOccurred())
			replicaNamespace, err := env.CreateUniqueNamespace(replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(replicaNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(replicaNamespace)
			})
			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(replicaNamespace, "backup-storage-creds", "minio", "minio123")
			})

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, replicaNamespace)
				Expect(err).ToNot(HaveOccurred())
			})

			AssertCreateCluster(replicaNamespace, srcClusterName, srcClusterSample, env)

			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterSample,
				testTableName,
				psqlClientPod)

			// Get primary from replica cluster
			primaryReplicaCluster, err := env.GetClusterPrimary(replicaNamespace, replicaClusterName)
			Expect(err).ToNot(HaveOccurred())

			commandTimeout := time.Second * 10

			By("verify archive mode is set to 'always on' designated primary", func() {
				query := "show archive_mode;"
				Eventually(func() (string, error) {
					stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
						&commandTimeout, "psql", "-U", "postgres", sourceDBName, "-tAc", query)
					return strings.Trim(stdOut, "\n"), err
				}, 30).Should(BeEquivalentTo("always"))
			})
			By("verify the WALs are archived from the designated primary", func() {
				// only replica cluster has backup configure to minio,
				// need the server name  be replica cluster name here
				AssertArchiveWalOnMinio(replicaNamespace, srcClusterName, replicaClusterName)
			})
		})
	})

	Context("can bootstrap a replica cluster from a backup", Ordered, func() {
		const (
			clusterSample   = fixturesDir + replicaModeClusterDir + "cluster-replica-src-with-backup.yaml.template"
			namespacePrefix = "replica-cluster-from-backup"
		)
		var namespace, clusterName string

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		BeforeAll(func() {
			var err error
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error { return env.DeleteNamespace(namespace) })

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})

			By("create the certificates for MinIO", func() {
				err := minioEnv.CreateCaSecret(env, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the cluster
			clusterName, err = env.GetResourceNameFromYAML(clusterSample)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, clusterSample, env)
		})

		It("using a Backup from the object store", func() {
			const (
				replicaClusterSample = fixturesDir + replicaModeClusterDir + "cluster-replica-from-backup.yaml.template"
				testTableName        = "replica_mode_backup"
			)

			By("creating a backup and waiting until it's completed", func() {
				backupName := fmt.Sprintf("%v-backup", clusterName)
				backup, err := testUtils.CreateOnDemandBackup(
					namespace,
					clusterName,
					backupName,
					apiv1.BackupTargetStandby,
					apiv1.BackupMethodBarmanObjectStore,
					env)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() (apiv1.BackupPhase, error) {
					err = env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      backupName,
					}, backup)
					return backup.Status.Phase, err
				}, testTimeouts[testUtils.BackupIsReady]).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
			})

			By("creating a replica cluster from the backup", func() {
				AssertReplicaModeCluster(
					namespace,
					clusterName,
					sourceDBName,
					replicaClusterSample,
					testTableName,
					psqlClientPod)
			})
		})

		It("using a Volume Snapshot", func() {
			const (
				replicaClusterSample = fixturesDir + replicaModeClusterDir + "cluster-replica-from-snapshot.yaml.template"
				snapshotDataEnv      = "REPLICA_CLUSTER_SNAPSHOT_NAME_PGDATA"
				snapshotWalEnv       = "REPLICA_CLUSTER_SNAPSHOT_NAME_PGWAL"
				testTableName        = "replica_mode_snapshot"
			)

			DeferCleanup(func() error {
				err := os.Unsetenv(snapshotDataEnv)
				if err != nil {
					return err
				}
				err = os.Unsetenv(snapshotWalEnv)
				if err != nil {
					return err
				}
				return nil
			})

			var backup *apiv1.Backup
			By("creating a snapshot and waiting until it's completed", func() {
				var err error
				snapshotName := fmt.Sprintf("%v-snapshot", clusterName)
				backup, err = testUtils.CreateOnDemandBackup(
					namespace,
					clusterName,
					snapshotName,
					apiv1.BackupTargetStandby,
					apiv1.BackupMethodVolumeSnapshot,
					env)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func(g Gomega) {
					err = env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      snapshotName,
					}, backup)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
					g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted))
				}, testTimeouts[testUtils.VolumeSnapshotIsReady]).Should(Succeed())
			})

			By("fetching the volume snapshots", func() {
				snapshotList := volumesnapshot.VolumeSnapshotList{}
				err := env.Client.List(env.Ctx, &snapshotList, k8client.MatchingLabels{
					utils.ClusterLabelName: clusterName,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))

				envVars := testUtils.EnvVarsForSnapshots{
					DataSnapshot: snapshotDataEnv,
					WalSnapshot:  snapshotWalEnv,
				}
				err = testUtils.SetSnapshotNameAsEnv(&snapshotList, backup, envVars)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating a replica cluster from the snapshot", func() {
				AssertReplicaModeCluster(
					namespace,
					clusterName,
					sourceDBName,
					replicaClusterSample,
					testTableName,
					psqlClientPod)
			})
		})
	})
})
