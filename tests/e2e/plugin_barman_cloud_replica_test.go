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
	"path"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	replicationasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin port of the "bootstrap a replica cluster from a backup" scenario: a
// source cluster archives through plugin-barman-cloud, then a replica cluster
// is bootstrapped from that backup via externalClusters[].plugin and streams
// from the source. It is added alongside the in-core variant (which shares its
// source cluster with a volume-snapshot test), and stays until the in-core
// Barman Cloud support is removed. Runs on kind/k3d only, where the plugin and
// the shared object store are installed.
var _ = Describe("plugin-barman-cloud replica cluster from backup",
	Label(tests.LabelPluginBarmanCloud, tests.LabelReplication, tests.LabelBackupRestore), func() {
		const (
			srcManifest     = fixturesDir + "/replica_mode_cluster/cluster-replica-src-with-plugin.yaml.template"
			replicaManifest = fixturesDir + "/replica_mode_cluster/cluster-replica-from-plugin-backup.yaml.template"
			backupManifest  = fixturesDir + "/replica_mode_cluster/backup-cluster-replica-src-plugin.yaml"
			srcClusterName  = "cluster-replica-src-plugin"
			srcDBName       = "appSrc"
			testTableName   = "replica_mode_backup"
			level           = tests.High
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
		})

		It("bootstraps a replica cluster from a plugin backup", func() {
			const namespacePrefix = "replica-cluster-from-plugin-backup"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, srcClusterName)

			By("creating the source cluster that archives through the plugin", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, srcClusterName, srcManifest)
			})

			By("taking a backup of the source through the plugin", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupManifest, false,
					testTimeouts[timeouts.BackupIsReady])
			})

			By("bootstrapping a replica cluster from the plugin backup", func() {
				replicationasserts.AssertReplicaModeCluster(env, testTimeouts, namespace,
					srcClusterName, srcDBName, replicaManifest, testTableName)
			})
		})
	})

// Plugin port of the "Replica switchover" scenario:
// In this test we create a replica cluster from a backup and then promote it to a primary.
// We expect the original primary to be demoted to a replica and be able to follow the new primary.
// Runs on kind/k3d only, where the plugin and the shared object store are installed.
//
//nolint:dupl // TODO: remove once in-tree counterpart is removed
var _ = Describe("plugin-barman-cloud replica cluster promotion/demotion",
	Label(tests.LabelPluginBarmanCloud, tests.LabelReplication, tests.LabelBackupRestore), Ordered, func() {
		const (
			localFixturesDir       = fixturesDir + "/replica_mode_cluster/pbc-promotion-demotion/"
			clusterAFileRestart    = localFixturesDir + "cluster-replica-switchover-restart-1.yaml.template"
			clusterBFileRestart    = localFixturesDir + "cluster-replica-switchover-restart-2.yaml.template"
			clusterAFileSwitchover = localFixturesDir + "cluster-replica-switchover-switchover-1.yaml.template"
			clusterBFileSwitchover = localFixturesDir + "cluster-replica-switchover-switchover-2.yaml.template"
			objectStoreName        = "replica-cluster-backups"
			level                  = tests.Medium
		)

		BeforeAll(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
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
			_ = objectstoreasserts.SwitchWalAndGetLatestArchive(env, namespace, primary.Name)

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

		DescribeTable(
			"should demote and promote the clusters correctly",
			func(clusterAFile string, clusterBFile string, expectedTimeline int) {
				const namespacePrefix = "replica-cluster-switchover"
				namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				clusterAName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterAFile)
				Expect(err).ToNot(HaveOccurred())
				clusterBName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterBFile)
				Expect(err).ToNot(HaveOccurred())

				DeferCleanup(func() error {
					// Since we use multiple times the same cluster names for the same object store instance, we need
					// to clean it up between tests
					_, err = objectstore.CleanFiles(objectStoreEnv, path.Join(objectStoreName, clusterAName))
					if err != nil {
						return err
					}
					_, err = objectstore.CleanFiles(objectStoreEnv, path.Join(objectStoreName, clusterBName))
					if err != nil {
						return err
					}
					return nil
				})

				stopLoad := make(chan struct{})
				DeferCleanup(func() { close(stopLoad) })

				setupPluginObjectStore(namespace, objectStoreName)

				By("creating the A cluster", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterAName, clusterAFile)
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
								Method:  apiv1.BackupMethodPlugin,
								Cluster: apiv1.LocalObjectReference{Name: clusterAName},
								PluginConfiguration: &apiv1.BackupPluginConfiguration{
									Name: "barman-cloud.cloudnative-pg.io",
								},
							},
						},
					)
					Expect(err).ToNot(HaveOccurred())

					// Speed up backup finalization
					primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterAName)
					Expect(err).ToNot(HaveOccurred())
					_ = objectstoreasserts.SwitchWalAndGetLatestArchive(env, namespace, primary.Name)
					Expect(err).ToNot(HaveOccurred())

					Eventually(func() (apiv1.BackupPhase, error) {
						err = env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      clusterAName,
						}, backup)
						return backup.Status.Phase, err
					}, testTimeouts[timeouts.BackupIsReady]).WithPolling(10 * time.Second).
						Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
				})

				By("creating the B cluster from the backup", func() {
					clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterBName, clusterBFile)
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
						pgasserts.AssertPgRecoveryMode(env, &pod, true)
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
					pgasserts.AssertPgRecoveryMode(env, primary, false)
					podList, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterBName)
					Expect(err).ToNot(HaveOccurred())
					for _, pod := range podList.Items {
						pgasserts.AssertPgRecoveryMode(env, &pod, true)
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
