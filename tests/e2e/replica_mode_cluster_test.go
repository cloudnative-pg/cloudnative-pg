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
	"strings"
	"time"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/thoas/go-funk"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
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
		})
	})

	Context("can bootstrap a replica cluster using basic auth", func() {
		It("can be detached from the source cluster", func() {
			const (
				replicaClusterSampleBasicAuth = fixturesDir + replicaModeClusterDir + "cluster-replica-basicauth.yaml.template"
				replicaNamespacePrefix        = "replica-mode-basic-auth"
				testTableName                 = "replica_mode_basic_auth"
			)

			replicaClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, replicaClusterSampleBasicAuth)
			Expect(err).ToNot(HaveOccurred())
			replicaNamespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(replicaNamespace, srcClusterName, srcClusterSample, env)

			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterSampleBasicAuth,
				testTableName,
			)

			AssertDetachReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				sourceDBName,
				replicaClusterName,
				replicaDBName,
				replicaUser,
				"replica_mode_basic_auth_detach")
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
		var namespace, clusterName string

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				namespaces.DumpNamespaceObjects(env.Ctx, env.Client, namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

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
				backup, err := backups.CreateOnDemand(
					env.Ctx,
					env.Client,
					namespace,
					clusterName,
					backupName,
					apiv1.BackupTargetStandby,
					apiv1.BackupMethodBarmanObjectStore,
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
				backup, err = backups.CreateOnDemand(
					env.Ctx,
					env.Client,
					namespace,
					clusterName,
					snapshotName,
					apiv1.BackupTargetStandby,
					apiv1.BackupMethodVolumeSnapshot,
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
				snapshotList := volumesnapshot.VolumeSnapshotList{}
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
