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
	"strconv"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configuration update", Label(tests.LabelClusterMetadata), func() {
	const (
		clusterName = "pg-configuration-update"
		level       = tests.High
	)
	var (
		namespace string
		targetTag string
	)

	postgresParams := map[string]string{
		"work_mem":                    "8MB",
		"max_connections":             "110",
		"log_checkpoints":             "on",
		"log_lock_waits":              "on",
		"log_min_duration_statement":  "1000",
		"log_statement":               "ddl",
		"log_temp_files":              "1024",
		"log_autovacuum_min_duration": "1s",
		"log_replication_commands":    "on",
		"wal_receiver_timeout":        "2s",
	}
	updateClusterPostgresParams := func(paramsMap map[string]string, namespace string) {
		cluster := &apiv1.Cluster{}
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Spec.PostgresConfiguration.Parameters = paramsMap
			return env.Client.Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	}

	updateClusterPostgresPgHBA := func(namespace string) {
		cluster := &apiv1.Cluster{}
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Spec.PostgresConfiguration.PgHBA = []string{"host all all all trust"}
			return env.Client.Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	}

	updateClusterPostgresPgIdent := func(namespace string) {
		cluster := &apiv1.Cluster{}
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Spec.PostgresConfiguration.PgIdent = []string{"email /^(.*)@example\\.com \\1"}
			return env.Client.Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	}

	checkErrorOutFixedAndBlockedConfigurationParameter := func(params map[string]string, namespace string) {
		// Update the configuration
		cluster := &apiv1.Cluster{}
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			cluster.Spec.PostgresConfiguration.Parameters = params
			cluster.Spec.PostgresConfiguration.Parameters["autovacuum_max_workers"] = "4"
			return env.Client.Update(env.Ctx, cluster)
		})
		Expect(apierrors.IsInvalid(err)).To(BeTrue())

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		// Expect other config parameters applied together with a blockedParameter to not have changed
		for _, pod := range podList.Items {
			Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
				"SHOW autovacuum_max_workers", "4"), RetryTimeout).ShouldNot(Succeed())
		}
	}

	checkSwitchoverOccurred := func(namespace, oldPrimary string) {
		By("verifying that a switchover happened", func() {
			Eventually(func() (string, error) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				return cluster.Status.CurrentPrimary, err
			}, 300).ShouldNot(BeEquivalentTo(oldPrimary))
		})
	}

	checkSwitchoverHaveNotOccurred := func(namespace, oldPrimary string) {
		By("verifying that a switchover didn't happen", func() {
			Consistently(func() (string, error) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				return cluster.Status.CurrentPrimary, err
			}, 5).Should(BeEquivalentTo(oldPrimary))
		})
	}

	gatherCurrentPrimary := func(namespace string) string {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
		return cluster.Status.CurrentPrimary
	}

	generateBaseCluster := func(namespace string) *apiv1.Cluster {
		storageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
		Expect(storageClass).ToNot(BeEmpty())

		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					StorageClass: &storageClass,
					Size:         "1Gi",
				},
				WalStorage: &apiv1.StorageConfiguration{
					StorageClass: &storageClass,
					Size:         "1Gi",
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_connections":             "110",
						"log_checkpoints":             "on",
						"log_lock_waits":              "on",
						"log_min_duration_statement":  "1000",
						"log_statement":               "ddl",
						"log_temp_files":              "1024",
						"log_autovacuum_min_duration": "1000",
						"log_replication_commands":    "on",
					},
				},
			},
		}
	}

	assertReloadGucs := func(namespace string) {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("apply configuration update", func() {
			updateClusterPostgresParams(postgresParams, namespace)
		})

		By("verify that work_mem result as expected", func() {
			// Check that GUCs has been modified in every pod
			for _, pod := range podList.Items {
				Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
					"SHOW work_mem", "8MB"), RetryTimeout).Should(Succeed())
			}
		})
	}

	assertChangeImageAndGucs := func(namespace string, primaryUpdateMethod apiv1.PrimaryUpdateMethod) {
		currentVersion, err := version.FromTag(env.PostgresImageTag)
		Expect(err).NotTo(HaveOccurred())
		defaultVersion, err := version.FromTag(reference.New(versions.DefaultImageName).Tag)
		Expect(err).NotTo(HaveOccurred())
		// Skip this test for development PostgreSQL versions (newer than default)
		// because they may not have compatible extensions like pgaudit available.
		// See https://github.com/cloudnative-pg/cloudnative-pg/issues/9331
		if currentVersion.Major() > defaultVersion.Major() {
			Skip("Running on a version newer than the default image, skipping this test")
		}

		cluster := &apiv1.Cluster{}
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			cluster.Spec.ImageName = env.StandardImageName(targetTag)
			cluster.Spec.PostgresConfiguration.Parameters["pgaudit.log"] = "all, -misc"
			cluster.Spec.PostgresConfiguration.Parameters["pgaudit.log_catalog"] = "off"
			cluster.Spec.PostgresConfiguration.Parameters["pgaudit.log_parameter"] = "on"
			cluster.Spec.PostgresConfiguration.Parameters["pgaudit.log_relation"] = "on"
			return env.Client.Update(env.Ctx, cluster)
		})

		if primaryUpdateMethod == apiv1.PrimaryUpdateMethodSwitchover {
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		}

		if primaryUpdateMethod == apiv1.PrimaryUpdateMethodRestart {
			Expect(err).NotTo(HaveOccurred())
			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			By("verify that pgaudit is enabled", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				QueryMatchExpectationPredicate(primary, postgres.PostgresDBName,
					"SELECT extname FROM pg_extension WHERE extname = 'pgaudit'", "pgaudit")
				QueryMatchExpectationPredicate(primary, postgres.PostgresDBName, "SHOW pgaudit.log", "all, -misc")
				QueryMatchExpectationPredicate(primary, postgres.PostgresDBName, "SHOW pgaudit.log_catalog", "off")
				QueryMatchExpectationPredicate(primary, postgres.PostgresDBName, "SHOW pgaudit.log_parameter", "on")
				QueryMatchExpectationPredicate(primary, postgres.PostgresDBName, "SHOW pgaudit.log_relation", "on")
			})
		}
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// TODO: remove this once all E2Es run on minimal images
		// https://github.com/cloudnative-pg/cloudnative-pg/issues/8123
		targetTag = strings.Split(env.PostgresImageTag, "-")[0]
	})

	Context("PrimaryUpdateMethod: switchover", Ordered, func() {
		BeforeAll(func() {
			namespacePrefix := "cluster-update-config-switchover"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			cluster := generateBaseCluster(namespace)
			cluster.Spec.ImageName = env.MinimalImageName(targetTag)
			cluster.Spec.PrimaryUpdateMethod = apiv1.PrimaryUpdateMethodSwitchover
			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			AssertClusterIsReady(cluster.Namespace, cluster.Name, testTimeouts[timeouts.ClusterIsReady], env)
		})

		It("1. reloading PG when a GUC requiring reload is modified", func() {
			assertReloadGucs(namespace)
		})

		It("2. reloading PG when pg_hba rules are modified", func() {
			endpointName := clusterName + "-rw"
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			// Connection should fail now because we are not supplying a password
			By("verify that connections with an empty password fail by default", func() {
				commandTimeout := time.Second * 10
				_, _, err := exec.Command(env.Ctx, env.Interface, env.RestClientConfig, podList.Items[0],
					specs.PostgresContainerName, &commandTimeout,
					"psql", "-U", "postgres", "-h", endpointName, "-tAc", "select 1",
				)
				Expect(err).To(HaveOccurred())
			})

			By("apply configuration update", func() {
				updateClusterPostgresPgHBA(namespace)
			})

			By("verify that connections succeed after updating pg_hba", func() {
				// The new pg_hba rule should be present in every pod
				query := "select count(*) from pg_catalog.pg_hba_file_rules where type = 'host' and auth_method = 'trust'"
				for _, pod := range podList.Items {
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						query, "1"), RetryTimeout).Should(Succeed())
				}
				// The connection should now work
				AssertConnection(namespace, endpointName, postgres.PostgresDBName, postgres.PostgresDBName, "", env)
			})
		})

		It("3. performing a rolling update when a GUC requiring restart is modified", func() {
			oldPrimary := gatherCurrentPrimary(namespace)

			By("apply configuration update", func() {
				postgresParams["shared_buffers"] = "256MB"
				updateClusterPostgresParams(postgresParams, namespace)
			})

			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			By("verify that shared_buffers setting changed", func() {
				// Check that the new parameter has been modified in every pod
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						"SHOW shared_buffers", "256MB"), RetryTimeout).Should(Succeed())
				}
			})

			checkSwitchoverOccurred(namespace, oldPrimary)
		})

		It("4. performing a rolling update when mixed parameters are modified", func() {
			oldPrimary := gatherCurrentPrimary(namespace)

			By("apply configuration update", func() {
				postgresParams["max_replication_slots"] = "16"
				postgresParams["maintenance_work_mem"] = "128MB"
				updateClusterPostgresParams(postgresParams, namespace)
			})

			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			By("verify that both parameters have been modified in each pod", func() {
				// Check that both parameters have been modified in each pod
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						"SHOW max_replication_slots", "16"), RetryTimeout).Should(Succeed())
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						"SHOW maintenance_work_mem", "128MB"), RetryTimeout).Should(Succeed())
				}
			})

			checkSwitchoverOccurred(namespace, oldPrimary)
		})

		It("5. erroring out when a fixedConfigurationParameter is modified", func() {
			postgresParams["cluster_name"] = "Setting this parameter is not allowed"
			checkErrorOutFixedAndBlockedConfigurationParameter(postgresParams, namespace)
		})

		It("6. erroring out when a blockedConfigurationParameter is modified", func() {
			delete(postgresParams, "cluster_name")
			postgresParams["port"] = "5433"
			checkErrorOutFixedAndBlockedConfigurationParameter(postgresParams, namespace)
		})

		It("7. restarting (no switch) when decreasing a hot-standby sensible GUC requiring a primary restart", func() {
			oldPrimary := gatherCurrentPrimary(namespace)

			By("apply configuration update", func() {
				// Update the configuration
				delete(postgresParams, "port")
				// max_connection decrease to 105
				postgresParams["max_connections"] = "105"
				updateClusterPostgresParams(postgresParams, namespace)
			})

			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			By("verify that max_connections has been decreased in every pod", func() {
				// Check that the new GUC has been modified in every pod
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						"SHOW max_connections", "105"), RetryTimeout).Should(Succeed())
				}
			})

			checkSwitchoverHaveNotOccurred(namespace, oldPrimary)
		})

		It("8. restarting (no switch) when reducing to default a hot-standby sensible GUC needing a primary restart", func() {
			oldPrimary := gatherCurrentPrimary(namespace)

			By("apply configuration update", func() {
				delete(postgresParams, "max_connections")
				updateClusterPostgresParams(postgresParams, namespace)
			})

			AssertClusterEventuallyReachesPhase(namespace, clusterName,
				[]string{apiv1.PhaseApplyingConfiguration, apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 30)
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			By("verify that the max_connections has been set to default in every pod", func() {
				// Check that the new parameter has been modified in every pod
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						"SHOW max_connections", "100"), RetryTimeout).Should(Succeed())
				}
			})

			checkSwitchoverHaveNotOccurred(namespace, oldPrimary)
		})

		It("9. reloading PG when pg_ident rules are modified", func() {
			// pg_ident_file_mappings is available from v15 only
			if env.PostgresVersion > 14 {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				query := "select count(1) from pg_catalog.pg_ident_file_mappings;"

				By("check that there is the expected number of entry in pg_ident_file_mappings", func() {
					Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
						query, "3"), RetryTimeout).Should(Succeed())
				})

				By("apply configuration update", func() {
					updateClusterPostgresPgIdent(namespace)
				})

				By("verify that there is one more entry in pg_ident_file_mappings", func() {
					Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
						query, "4"), RetryTimeout).Should(Succeed())
				})
			}
		})

		It("10. performing a rolling update when changing imageName and extension GUC at the same time", func() {
			assertChangeImageAndGucs(namespace, apiv1.PrimaryUpdateMethodSwitchover)
		})
	})

	Context("PrimaryUpdateMethod: restart", Ordered, func() {
		BeforeAll(func() {
			namespacePrefix := "cluster-update-config-restart"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			cluster := generateBaseCluster(namespace)
			cluster.Spec.ImageName = env.MinimalImageName(targetTag)
			cluster.Spec.PrimaryUpdateMethod = apiv1.PrimaryUpdateMethodRestart
			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			AssertClusterIsReady(cluster.Namespace, cluster.Name, testTimeouts[timeouts.ClusterIsReady], env)
		})

		It("1. reloading PG when a GUC requiring reload is modified", func() {
			assertReloadGucs(namespace)
		})

		It("2. restarting (in place) the primary after increasing max_connection", func() {
			// Ensure cluster is fully ready after previous test configuration change
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)

			var oldPrimaryPodName string
			var newMaxConnectionsValue int
			var primaryStartTime time.Time

			By("getting old primary info", func() {
				oldPrimaryPodName = gatherCurrentPrimary(namespace)

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
				Expect(err).ToNot(HaveOccurred())
				defer func() {
					// Here we need to close the connection and close the forward, if we don't do both steps
					// the PostgreSQL connection will be there and PostgreSQL will not restart in time because
					// of the connection that wasn't close and stays idle
					_ = conn.Close()
					forward.Close()
				}()

				query := "SELECT TO_CHAR(pg_postmaster_start_time(), 'YYYY-MM-DD HH24:MI:SS');"
				var startTime string
				row := conn.QueryRow(query)
				err = row.Scan(&startTime)
				Expect(err).ToNot(HaveOccurred())

				primaryStartTime, err = cnpgTypes.ParseTargetTime(nil, startTime)
				Expect(err).NotTo(HaveOccurred())

				query = "show max_connections"
				row = conn.QueryRow(query)
				var maxConnections int
				err = row.Scan(&maxConnections)
				Expect(err).ToNot(HaveOccurred())
				newMaxConnectionsValue = maxConnections + 10
			})

			By(fmt.Sprintf("updating max_connection value to %v", newMaxConnectionsValue), func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				updated.Spec.PostgresConfiguration.Parameters["max_connections"] = fmt.Sprintf("%v",
					newMaxConnectionsValue)
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying the new value for max_connections is updated for all instances", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				for _, pod := range podList.Items {
					Eventually(QueryMatchExpectationPredicate(&pod, postgres.PostgresDBName,
						"SHOW max_connections", strconv.Itoa(newMaxConnectionsValue)), 180).Should(Succeed())
				}
			})

			checkSwitchoverHaveNotOccurred(namespace, oldPrimaryPodName)

			By("verifying that old primary was actually restarted", func() {
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      oldPrimaryPodName,
				}, &pod)
				Expect(err).ToNot(HaveOccurred())

				// take pg postmaster start time
				query := "select to_char(pg_postmaster_start_time(), 'YYYY-MM-DD HH24:MI:SS');"
				stdout, _, cmdErr := exec.EventuallyExecQueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: pod.Namespace,
						PodName:   pod.Name,
					}, postgres.PostgresDBName,
					query,
					RetryTimeout,
					PollingTime,
				)
				Expect(cmdErr).ToNot(HaveOccurred())

				newStartTime, err := cnpgTypes.ParseTargetTime(nil, strings.Trim(stdout, "\n"))
				Expect(err).NotTo(HaveOccurred())

				// verify that pg postmaster start time is greater than currentTimestamp which was taken before restart
				Expect(newStartTime).Should(BeTemporally(">", primaryStartTime))
			})
		})

		It("3. performing a rolling update when changing imageName and extension GUC at the same time", func() {
			oldPrimary := gatherCurrentPrimary(namespace)
			assertChangeImageAndGucs(namespace, apiv1.PrimaryUpdateMethodRestart)
			checkSwitchoverHaveNotOccurred(namespace, oldPrimary)
		})
	})
})
