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
	"os"
	"path"
	"strings"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

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

	var err error
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

			replicaNamespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(replicaNamespace, srcClusterName, srcClusterSample, env)

			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterSampleTLS,
				testTableName,
			)

			replicaName, err := yaml.GetResourceNameFromYAML(env.Scheme, replicaClusterSampleTLS)
			Expect(err).ToNot(HaveOccurred())

			assertReplicaClusterTopology(replicaNamespace, replicaName)

			AssertSwitchoverOnReplica(replicaNamespace, replicaName, env)

			assertReplicaClusterTopology(replicaNamespace, replicaName)

			By("increasing max_connections to 120 on the replica cluster", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, replicaNamespace, replicaName)
					if err != nil {
						return err
					}
					if cluster.Spec.PostgresConfiguration.Parameters == nil {
						cluster.Spec.PostgresConfiguration.Parameters = map[string]string{}
					}
					cluster.Spec.PostgresConfiguration.Parameters["max_connections"] = "120"
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for the replica cluster to apply the increased max_connections", func() {
				AssertClusterEventuallyReachesPhase(replicaNamespace, replicaName,
					[]string{
						apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade,
						apiv1.PhaseWaitingForInstancesToBeActive,
					}, 30)
				AssertClusterIsReady(replicaNamespace, replicaName,
					testTimeouts[timeouts.ClusterIsReadyQuick], env)
			})

			By("verifying max_connections is 120 on all pods", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, replicaNamespace, replicaName)
				Expect(err).ToNot(HaveOccurred())
				for idx := range podList.Items {
					pod := &podList.Items[idx]
					Eventually(QueryMatchExpectationPredicate(
						pod, postgres.PostgresDBName, "SHOW max_connections", "120"),
						30).Should(Succeed())
				}
			})

			By("decreasing max_connections to 110 on the replica cluster", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, replicaNamespace, replicaName)
					if err != nil {
						return err
					}
					cluster.Spec.PostgresConfiguration.Parameters["max_connections"] = "110"
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			By("waiting for the replica cluster to apply the decreased max_connections", func() {
				AssertClusterEventuallyReachesPhase(replicaNamespace, replicaName,
					[]string{
						apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade,
						apiv1.PhaseWaitingForInstancesToBeActive,
					}, 30)
				AssertClusterIsReady(replicaNamespace, replicaName,
					testTimeouts[timeouts.ClusterIsReadyQuick], env)
			})

			By("verifying max_connections is 110 on all pods", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, replicaNamespace, replicaName)
				Expect(err).ToNot(HaveOccurred())
				for idx := range podList.Items {
					pod := &podList.Items[idx]
					Eventually(QueryMatchExpectationPredicate(
						pod, postgres.PostgresDBName, "SHOW max_connections", "110"),
						30).Should(Succeed())
				}
			})
		})
	})

	Context("can bootstrap a replica cluster using basic auth", func() {
		var namespace string

		It("can be detached from the source cluster", func() {
			const (
				replicaClusterSampleBasicAuth = fixturesDir + replicaModeClusterDir + "cluster-replica-basicauth.yaml.template"
				replicaNamespacePrefix        = "replica-mode-basic-auth"
				testTableName                 = "replica_mode_basic_auth"
			)

			replicaClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, replicaClusterSampleBasicAuth)
			Expect(err).ToNot(HaveOccurred())
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, srcClusterName, srcClusterSample, env)

			AssertReplicaModeCluster(
				namespace,
				srcClusterName,
				sourceDBName,
				replicaClusterSampleBasicAuth,
				testTableName,
			)

			AssertDetachReplicaModeCluster(
				namespace,
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

			getReplicaClusterSwitchCondition := func(conds []metav1.Condition) *metav1.Condition {
				for _, condition := range conds {
					if condition.Type == conditions.ReplicaClusterSwitch {
						return &condition
					}
				}
				return nil
			}

			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "replica-promotion-demotion")
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterOneName, clusterOneFile, env)

			AssertReplicaModeCluster(
				namespace,
				clusterOneName,
				sourceDBName,
				clusterTwoFile,
				testTableName,
			)

			// turn the src cluster into a replica
			By("setting replica mode on the src cluster", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterOneName)
				Expect(err).ToNot(HaveOccurred())
				updateTime := time.Now().Truncate(time.Second)
				cluster.Spec.ReplicaCluster.Enabled = ptr.To(true)
				err = env.Client.Update(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterOneName)
					g.Expect(err).ToNot(HaveOccurred())
					condition := getReplicaClusterSwitchCondition(cluster.Status.Conditions)
					g.Expect(condition).ToNot(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.LastTransitionTime.Time).To(BeTemporally(">=", updateTime))
				}).WithTimeout(30 * time.Second).Should(Succeed())
				AssertClusterIsReady(namespace, clusterOneName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("checking that src cluster is now a replica cluster", func() {
				Eventually(func() error {
					clusterOnePrimary, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
						clusterOneName)
					return err
				}, 30, 3).Should(Succeed())
				AssertPgRecoveryMode(clusterOnePrimary, true)
			})

			// turn the dst cluster into a primary
			By("disabling the replica mode on the dst cluster", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterTwoName)
				Expect(err).ToNot(HaveOccurred())
				cluster.Spec.ReplicaCluster.Enabled = ptr.To(false)
				err = env.Client.Update(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
				AssertClusterIsReady(namespace, clusterTwoName, testTimeouts[timeouts.ClusterIsReady], env)
			})

			By("checking that dst cluster has been promoted", func() {
				Eventually(func() error {
					clusterTwoPrimary, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
						clusterTwoName)
					return err
				}, 30, 3).Should(Succeed())
				AssertPgRecoveryMode(clusterTwoPrimary, false)
			})

			By("creating a new data in the new source cluster", func() {
				tableLocator := TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterTwoName,
					DatabaseName: sourceDBName,
					TableName:    "new_test_table",
				}
				Eventually(func() error {
					_, err := postgres.RunExecOverForward(ctx,
						env.Client,
						env.Interface,
						env.RestClientConfig,
						namespace, clusterTwoName, sourceDBName,
						apiv1.ApplicationUserSecretSuffix,
						"SELECT 1;",
					)
					return err
				}, testTimeouts[timeouts.Short]).Should(Succeed())
				AssertCreateTestData(env, tableLocator)
			})

			// The dst Cluster gets promoted to primary, hence the new appUser password will
			// be updated to reflect its "-app" secret.
			// We need to copy the password changes over to the src Cluster, which is now a Replica
			// Cluster, in order to connect using the "-app" secret.
			By("updating the appUser secret of the src cluster", func() {
				_, appSecretPassword, err := secrets.GetCredentials(
					env.Ctx, env.Client,
					clusterTwoName, namespace,
					apiv1.ApplicationUserSecretSuffix)
				Expect(err).ToNot(HaveOccurred())
				AssertUpdateSecret("password", appSecretPassword, clusterOneName+apiv1.ApplicationUserSecretSuffix,
					namespace, clusterOneName, 30, env)
			})

			By("checking that the data is present in the old src cluster", func() {
				tableLocator := TableLocator{
					Namespace:    namespace,
					ClusterName:  clusterOneName,
					DatabaseName: sourceDBName,
					TableName:    "new_test_table",
				}
				AssertDataExpectedCount(env, tableLocator, 2)
			})
		})
	})

	Context("archive mode set to 'always' on designated primary", Label(tests.LabelBackupRestore), func() {
		It("verifies replica cluster can archive WALs from the designated primary", func() {
			const (
				replicaClusterSample   = fixturesDir + replicaModeClusterDir + "cluster-replica-archive-mode-always.yaml.template"
				replicaNamespacePrefix = "replica-mode-archive"
				testTableName          = "replica_mode_archive"
			)

			replicaClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, replicaClusterSample)
			Expect(err).ToNot(HaveOccurred())
			replicaNamespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("creating the credentials for minio", func() {
				_, err = secrets.CreateObjectStorageSecret(
					env.Ctx,
					env.Client,
					replicaNamespace,
					"backup-storage-creds",
					"minio",
					"minio123",
				)
				Expect(err).ToNot(HaveOccurred())
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
			)

			// Get primary from replica cluster
			primaryReplicaCluster, err := clusterutils.GetPrimary(
				env.Ctx,
				env.Client,
				replicaNamespace,
				replicaClusterName,
			)
			Expect(err).ToNot(HaveOccurred())

			By("verify archive mode is set to 'always on' designated primary", func() {
				query := "show archive_mode;"
				Eventually(func() (string, error) {
					stdOut, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: primaryReplicaCluster.Namespace,
							PodName:   primaryReplicaCluster.Name,
						},
						sourceDBName,
						query)
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

	Context("can bootstrap a replica cluster from a backup", Label(tests.LabelBackupRestore), Ordered, func() {
		const (
			clusterSample   = fixturesDir + replicaModeClusterDir + "cluster-replica-src-with-backup.yaml.template"
			namespacePrefix = "replica-cluster-from-backup"
		)
		var clusterName string
		var namespace string

		BeforeAll(func() {
			var err error
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
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterSample)
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
				backup, err := backups.Create(
					env.Ctx, env.Client,
					apiv1.Backup{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      backupName,
						},
						Spec: apiv1.BackupSpec{
							Target:  apiv1.BackupTargetStandby,
							Method:  apiv1.BackupMethodBarmanObjectStore,
							Cluster: apiv1.LocalObjectReference{Name: clusterName},
						},
					},
				)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() (apiv1.BackupPhase, error) {
					err = env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      backupName,
					}, backup)
					return backup.Status.Phase, err
				}, testTimeouts[timeouts.BackupIsReady]).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
			})

			By("creating a replica cluster from the backup", func() {
				AssertReplicaModeCluster(
					namespace,
					clusterName,
					sourceDBName,
					replicaClusterSample,
					testTableName,
				)
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
				backup, err = backups.Create(
					env.Ctx,
					env.Client,
					apiv1.Backup{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      snapshotName,
						},
						Spec: apiv1.BackupSpec{
							Target:  apiv1.BackupTargetStandby,
							Method:  apiv1.BackupMethodVolumeSnapshot,
							Cluster: apiv1.LocalObjectReference{Name: clusterName},
						},
					},
				)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func(g Gomega) {
					err = env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      snapshotName,
					}, backup)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(2))
					g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted))
				}, testTimeouts[timeouts.VolumeSnapshotIsReady]).Should(Succeed())
			})

			By("fetching the volume snapshots", func() {
				snapshotList := volumesnapshotv1.VolumeSnapshotList{}
				err := env.Client.List(env.Ctx, &snapshotList, k8client.MatchingLabels{
					utils.ClusterLabelName: clusterName,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Elements)))

				envVars := storage.EnvVarsForSnapshots{
					DataSnapshot: snapshotDataEnv,
					WalSnapshot:  snapshotWalEnv,
				}
				err = storage.SetSnapshotNameAsEnv(&snapshotList, backup, envVars)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating a replica cluster from the snapshot", func() {
				AssertReplicaModeCluster(
					namespace,
					clusterName,
					sourceDBName,
					replicaClusterSample,
					testTableName,
				)
			})
		})
	})
})

// In this test we create a replica cluster from a backup and then promote it to a primary.
// We expect the original primary to be demoted to a replica and be able to follow the new primary.
var _ = Describe("Replica switchover", Label(tests.LabelReplication, tests.LabelBackupRestore), Ordered, func() {
	const (
		replicaSwitchoverClusterDir = "/replica_mode_cluster/"
		namespacePrefix             = "replica-switchover"
		level                       = tests.Medium
		clusterAFileRestart         = fixturesDir + replicaSwitchoverClusterDir +
			"cluster-replica-switchover-restart-1.yaml.template"
		clusterBFileRestart = fixturesDir + replicaSwitchoverClusterDir +
			"cluster-replica-switchover-restart-2.yaml.template"
		clusterAFileSwitchover = fixturesDir + replicaSwitchoverClusterDir +
			"cluster-replica-switchover-switchover-1.yaml.template"
		clusterBFileSwitchover = fixturesDir + replicaSwitchoverClusterDir +
			"cluster-replica-switchover-switchover-2.yaml.template"
	)

	var namespace, clusterAName, clusterBName string

	BeforeAll(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	validateReplication := func(namespace, clusterAName, clusterBName string) {
		primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterBName)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: namespace, PodName: primary.Name},
			"postgres",
			"CREATE TABLE test_replication AS SELECT 1;",
		)
		Expect(err).ToNot(HaveOccurred())
		_ = switchWalAndGetLatestArchive(namespace, primary.Name)

		Eventually(func(g Gomega) {
			podListA, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterAName)
			g.Expect(err).ToNot(HaveOccurred())
			podListB, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterBName)
			g.Expect(err).ToNot(HaveOccurred())

			for _, podA := range podListA.Items {
				_, _, err = exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{Namespace: namespace, PodName: podA.Name},
					"postgres",
					"SELECT * FROM test_replication;",
				)
				g.Expect(err).ToNot(HaveOccurred())
			}

			for _, podB := range podListB.Items {
				_, _, err = exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{Namespace: namespace, PodName: podB.Name},
					"postgres",
					"SELECT * FROM test_replication;",
				)
				g.Expect(err).ToNot(HaveOccurred())
			}
		}, testTimeouts[timeouts.ClusterIsReadyQuick]).Should(Succeed())
	}

	waitForTimelineIncrease := func(namespace, clusterName string, expectedTimeline int) bool {
		return Eventually(func(g Gomega) {
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{Namespace: namespace, PodName: primary.Name},
				"postgres",
				"SELECT timeline_id FROM pg_catalog.pg_control_checkpoint()",
			)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(strings.TrimSpace(stdout)).To(Equal(fmt.Sprintf("%d", expectedTimeline)))
		}, testTimeouts[timeouts.ClusterIsReadyQuick]).Should(Succeed())
	}

	DescribeTable("should demote and promote the clusters correctly",
		func(clusterAFile string, clusterBFile string, expectedTimeline int) {
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				// Since we use multiple times the same cluster names for the same minio instance, we need to clean it up
				// between tests
				_, err = minio.CleanFiles(minioEnv, path.Join("minio", "cluster-backups", clusterAName))
				if err != nil {
					return err
				}
				_, err = minio.CleanFiles(minioEnv, path.Join("minio", "cluster-backups", clusterBName))
				if err != nil {
					return err
				}
				return nil
			})

			stopLoad := make(chan struct{})
			DeferCleanup(func() { close(stopLoad) })

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

			By("creating the A cluster", func() {
				var err error
				clusterAName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterAFile)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterAName, clusterAFile, env)
			})
			By("creating some load on the A cluster", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterAName)
				Expect(err).ToNot(HaveOccurred())
				_, _, err = exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{Namespace: namespace, PodName: primary.Name},
					"postgres",
					"CREATE TABLE switchover_load (i int);",
				)
				Expect(err).ToNot(HaveOccurred())

				go func() {
					for {
						_, _, _ = exec.QueryInInstancePod(
							env.Ctx, env.Client, env.Interface, env.RestClientConfig,
							exec.PodLocator{Namespace: namespace, PodName: primary.Name},
							"postgres",
							"INSERT INTO switchover_load SELECT generate_series(1, 10000)",
						)
						select {
						case <-stopLoad:
							GinkgoWriter.Println("Terminating load")
							return
						default:
							continue
						}
					}
				}()
			})

			By("backing up the A cluster", func() {
				backup, err := backups.Create(
					env.Ctx, env.Client,
					apiv1.Backup{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      clusterAName,
						},
						Spec: apiv1.BackupSpec{
							Target:  apiv1.BackupTargetPrimary,
							Method:  apiv1.BackupMethodBarmanObjectStore,
							Cluster: apiv1.LocalObjectReference{Name: clusterAName},
						},
					},
				)
				Expect(err).ToNot(HaveOccurred())

				// Speed up backup finalization
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterAName)
				Expect(err).ToNot(HaveOccurred())
				_ = switchWalAndGetLatestArchive(namespace, primary.Name)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func() (apiv1.BackupPhase, error) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      clusterAName,
						}, backup)
						return backup.Status.Phase, err
					},
					testTimeouts[timeouts.BackupIsReady],
				).WithPolling(10 * time.Second).
					Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
			})

			By("creating the B cluster from the backup", func() {
				var err error
				clusterBName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterBFile)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterBName, clusterBFile, env)
			})

			By("demoting A to a replica", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterAName)
				Expect(err).ToNot(HaveOccurred())
				oldCluster := cluster.DeepCopy()
				cluster.Spec.ReplicaCluster.Primary = clusterBName
				Expect(env.Client.Patch(env.Ctx, cluster, k8client.MergeFrom(oldCluster))).To(Succeed())
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterAName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					AssertPgRecoveryMode(&pod, true)
				}
			})

			var token, invalidToken string
			By("getting the demotion token", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterAName)
				Expect(err).ToNot(HaveOccurred())
				token = cluster.Status.DemotionToken
			})

			By("forging an invalid token", func() {
				tokenContent, err := utils.ParsePgControldataToken(token)
				Expect(err).ToNot(HaveOccurred())
				tokenContent.LatestCheckpointREDOLocation = "0/0"
				Expect(tokenContent.IsValid()).To(Succeed())
				invalidToken, err = tokenContent.Encode()
				Expect(err).ToNot(HaveOccurred())
			})

			By("promoting B with the invalid token", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterBName)
				Expect(err).ToNot(HaveOccurred())

				oldCluster := cluster.DeepCopy()
				cluster.Spec.ReplicaCluster.PromotionToken = invalidToken
				cluster.Spec.ReplicaCluster.Primary = clusterBName
				Expect(env.Client.Patch(env.Ctx, cluster, k8client.MergeFrom(oldCluster))).To(Succeed())
			})

			By("failing to promote B with the invalid token", func() {
				Consistently(func(g Gomega) {
					pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterBName)
					g.Expect(err).ToNot(HaveOccurred())
					stdOut, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: pod.Namespace,
							PodName:   pod.Name,
						},
						postgres.PostgresDBName,
						"select pg_catalog.pg_is_in_recovery()")
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(strings.Trim(stdOut, "\n")).To(Equal("t"))
				}, 60, 10).Should(Succeed())
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterBName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Status.Phase).To(BeEquivalentTo(apiv1.PhaseUnrecoverable))
			})

			By("promoting B with the right token", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterBName)
				Expect(err).ToNot(HaveOccurred())
				oldCluster := cluster.DeepCopy()
				cluster.Spec.ReplicaCluster.PromotionToken = token
				cluster.Spec.ReplicaCluster.Primary = clusterBName
				Expect(env.Client.Patch(env.Ctx, cluster, k8client.MergeFrom(oldCluster))).To(Succeed())
			})

			By("reaching the target timeline", func() {
				waitForTimelineIncrease(namespace, clusterBName, expectedTimeline)
			})

			By("verifying B contains the primary", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterBName)
				Expect(err).ToNot(HaveOccurred())
				AssertPgRecoveryMode(primary, false)
				podList, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterBName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					AssertPgRecoveryMode(&pod, true)
				}
			})

			By("verifying replication from new primary works everywhere", func() {
				validateReplication(namespace, clusterAName, clusterBName)
			})
		},
		Entry("when primaryUpdateMethod is set to restart", clusterAFileRestart, clusterBFileRestart, 2),
		Entry("when primaryUpdateMethod is set to switchover", clusterAFileSwitchover, clusterBFileSwitchover, 3),
	)
})

// assertReplicaClusterTopology asserts that the replica cluster topology is correct
// it verifies that the designated primary is streaming from the source
// and that the replicas are only streaming from the designated primary
func assertReplicaClusterTopology(namespace, clusterName string) {
	var (
		timeout        = 120
		commandTimeout = time.Second * 10

		sourceHost, primary string
		standbys            []string
	)

	cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	Expect(cluster.Status.ReadyInstances).To(BeEquivalentTo(cluster.Spec.Instances))

	Expect(cluster.Spec.ExternalClusters).Should(HaveLen(1))
	sourceHost = cluster.Spec.ExternalClusters[0].ConnectionParameters["host"]
	Expect(sourceHost).ToNot(BeEmpty())

	primary = cluster.Status.CurrentPrimary
	standbys = funk.FilterString(cluster.Status.InstanceNames, func(name string) bool { return name != primary })

	getStreamingInfo := func(podName string) ([]string, error) {
		stdout, _, err := exec.CommandInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: namespace,
				PodName:   podName,
			},
			&commandTimeout,
			"psql", "-U", "postgres", "-tAc",
			"select string_agg(application_name, ',') from pg_catalog.pg_stat_replication;",
		)
		if err != nil {
			return nil, err
		}
		stdout = strings.TrimSpace(stdout)
		if stdout == "" {
			return []string{}, nil
		}
		return strings.Split(stdout, ","), err
	}

	By("verifying that the standby is not streaming to any other instance", func() {
		Eventually(func(g Gomega) {
			for _, standby := range standbys {
				streamingInstances, err := getStreamingInfo(standby)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(streamingInstances).To(BeEmpty(),
					fmt.Sprintf("the standby %s should not stream to any other instance", standby),
				)
			}
		}, timeout).ShouldNot(HaveOccurred())
	})

	By("verifying that the new primary is streaming to all standbys", func() {
		Eventually(func(g Gomega) {
			streamingInstances, err := getStreamingInfo(primary)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(streamingInstances).To(
				ContainElements(standbys),
				"not all standbys are streaming from the new primary "+primary,
			)
		}, timeout).ShouldNot(HaveOccurred())
	})

	By("verifying that the new primary is streaming from the source cluster", func() {
		Eventually(func(g Gomega) {
			stdout, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   primary,
				},
				&commandTimeout,
				"psql", "-U", "postgres", "-tAc",
				"select sender_host from pg_catalog.pg_stat_wal_receiver limit 1",
			)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(strings.TrimSpace(stdout)).To(BeEquivalentTo(sourceHost))
		}, timeout).ShouldNot(HaveOccurred())
	})
}
