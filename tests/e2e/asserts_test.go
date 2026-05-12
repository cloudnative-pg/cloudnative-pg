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
	"regexp"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/importdb"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/proxy"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Update the secrets and verify cluster reference the updated resource version of secrets
func AssertUpdateSecret(
	field string,
	value string,
	secretName string,
	namespace string,
	clusterName string,
	timeout int,
	env *environment.TestingEnvironment,
) {
	var secret corev1.Secret

	// Gather the secret
	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx,
			ctrlclient.ObjectKey{Namespace: namespace, Name: secretName},
			&secret)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	// Change the given field to the new value provided
	secret.Data[field] = []byte(value)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return env.Client.Update(env.Ctx, &secret)
	})
	Expect(err).ToNot(HaveOccurred())

	// Wait for the cluster to pick up the updated secrets version first
	Eventually(func() string {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		if err != nil {
			GinkgoWriter.Printf("Error reports while retrieving cluster %v\n", err.Error())
			return ""
		}
		switch {
		case strings.HasSuffix(secretName, apiv1.ApplicationUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of %s secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.ApplicationSecretVersion)
			return cluster.Status.SecretsResourceVersion.ApplicationSecretVersion

		case strings.HasSuffix(secretName, apiv1.SuperUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of %s secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.SuperuserSecretVersion)
			return cluster.Status.SecretsResourceVersion.SuperuserSecretVersion

		case cluster.UsesSecretInManagedRoles(secretName):
			GinkgoWriter.Printf("Resource version of %s ManagedRole secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.ManagedRoleSecretVersions[secretName])
			return cluster.Status.SecretsResourceVersion.ManagedRoleSecretVersions[secretName]

		default:
			GinkgoWriter.Printf("Unsupported secrets name found %v\n", secretName)
			return ""
		}
	}, timeout).Should(BeEquivalentTo(secret.ResourceVersion))
}

// AssertConnection is used if a connection from a pod to a postgresql database works
// AssertClusterStandbysAreStreaming verifies that all the standbys of a cluster have a wal-receiver running.
func AssertClusterStandbysAreStreaming(namespace string, clusterName string, timeout int32) {
	query := "SELECT count(*) FROM pg_catalog.pg_stat_wal_receiver"
	Eventually(func() error {
		standbyPods, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
		if err != nil {
			return err
		}

		for _, pod := range standbyPods.Items {
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				postgres.PostgresDBName,
				query,
			)
			if err != nil {
				return err
			}

			value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
			if atoiErr != nil {
				return atoiErr
			}
			if value != 1 {
				return fmt.Errorf("pod %v not streaming", pod.Name)
			}
		}

		return nil
	}, timeout).ShouldNot(HaveOccurred())
}

func AssertStandbysFollowPromotion(namespace string, clusterName string, timeout int32) {
	// Track the start of the assertion. We expect to complete before
	// timeout.
	start := time.Now()

	By(fmt.Sprintf("having all the instances on timeline 2 in less than %v sec", timeout), func() {
		// One of the standbys will be promoted and the rw service
		// should point to it, so the application can keep writing.
		// Records inserted after the promotion will be marked
		// with timeline '00000002'. If all the instances are back
		// and are following the promotion, we should find those
		// records on each of them.

		for i := 1; i < 4; i++ {
			podName := fmt.Sprintf("%v-%v", clusterName, i)
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			query := "SELECT count(*) > 0 FROM tps.tl WHERE timeline = '00000002'"
			Eventually(func() (string, error) {
				pod := &corev1.Pod{}
				if err := env.Client.Get(env.Ctx, podNamespacedName, pod); err != nil {
					return "", err
				}
				out, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: pod.Namespace,
						PodName:   pod.Name,
					},
					postgres.AppDBName,
					query,
				)
				return strings.TrimSpace(out), err
			}, timeout).Should(BeEquivalentTo("t"),
				"Pod %v should have moved to timeline 2", podName)
		}
	})

	By("having all the instances ready", func() {
		clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
	})

	By(fmt.Sprintf("restoring full cluster functionality within %v seconds", timeout), func() {
		elapsed := time.Since(start)
		fmt.Printf("Cluster has been in a degraded state for %v seconds\n", elapsed)
		Expect(elapsed.Seconds()).To(BeNumerically("<", timeout))
	})
}

func AssertWritesResumedBeforeTimeout(namespace string, clusterName string, timeout int32) {
	By(fmt.Sprintf("resuming writing in less than %v sec", timeout), func() {
		// We measure the difference between the last entry with
		// timeline 1 and the first one with timeline 2.
		// It should be less than maxFailoverTime seconds.
		// Any pod is good to measure the difference, we choose -2
		query := "WITH a AS ( " +
			"  SELECT * " +
			"  , t-lag(t) OVER (order by t) AS timediff " +
			"  FROM tps.tl " +
			") " +
			"SELECT EXTRACT ('EPOCH' FROM timediff) " +
			"FROM a " +
			"WHERE timeline = ( " +
			"  SELECT timeline " +
			"  FROM tps.tl " +
			"  ORDER BY t DESC " +
			"  LIMIT 1 " +
			") " +
			"ORDER BY t ASC " +
			"LIMIT 1;"
		podName := clusterName + "-2"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		var switchTime float64
		pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, pod)
		Expect(err).ToNot(HaveOccurred())
		out, _, err := exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
			}, postgres.AppDBName,
			query,
			RetryTimeout,
			PollingTime,
		)
		Expect(err).ToNot(HaveOccurred())
		switchTime, err = strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err != nil {
			fmt.Printf("Write activity resumed in %v seconds\n", switchTime)
		}
		Expect(switchTime, err).Should(BeNumerically("<", timeout))
	})
}

// AssertNewPrimary checks that, during a failover, a new primary
// is being elected and promoted and that write operation succeed
// on this new pod.
// CheckPointAndSwitchWalOnPrimary trigger a checkpoint and switch wal on primary pod and returns the latest WAL file
// AssertReplicaModeCluster checks that, after inserting some data in a source cluster,
// a replica cluster can be bootstrapped using pg_basebackup and is properly replicating
// from the source cluster
func AssertReplicaModeCluster(
	namespace,
	srcClusterName,
	srcClusterDBName,
	replicaClusterSample,
	testTableName string,
) {
	var primaryReplicaCluster *corev1.Pod
	checkQuery := fmt.Sprintf("SELECT count(*) FROM %v", testTableName)

	tableLocator := pgasserts.TableLocator{
		Namespace:    namespace,
		ClusterName:  srcClusterName,
		DatabaseName: srcClusterDBName,
		TableName:    testTableName,
	}
	pgasserts.AssertCreateTestData(env, tableLocator)

	By("creating replica cluster", func() {
		replicaClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, replicaClusterSample)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, replicaClusterName, replicaClusterSample)
		// Get primary from replica cluster
		Eventually(func() error {
			primaryReplicaCluster, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
				replicaClusterName)
			return err
		}, 30, 3).Should(Succeed())
		pgasserts.AssertPgRecoveryMode(env, primaryReplicaCluster, true)
	})

	By("checking data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryReplicaCluster.Namespace,
					PodName:   primaryReplicaCluster.Name,
				},
				exec.DatabaseName(srcClusterDBName),
				checkQuery,
			)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 10).Should(BeEquivalentTo("2"))
	})

	By("writing some new data to the source cluster", func() {
		forwardSource, connSource, err := postgres.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			srcClusterName,
			srcClusterDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = connSource.Close()
			forwardSource.Close()
		}()
		Expect(err).ToNot(HaveOccurred())
		pgasserts.InsertRecordIntoTable(testTableName, 3, connSource)
	})

	By("checking new data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryReplicaCluster.Namespace,
					PodName:   primaryReplicaCluster.Name,
				},
				exec.DatabaseName(srcClusterDBName),
				checkQuery,
			)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 15).Should(BeEquivalentTo("3"))
	})

	if srcClusterDBName != "app" {
		// verify the replica database created followed the source database, rather than
		// default to the "app" db and user
		By("checking that in replica cluster there is no database app and user app", func() {
			Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, postgres.PostgresDBName,
				pgasserts.DatabaseExistsQuery("app"), "f"),
				30).Should(Succeed())
			Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, postgres.PostgresDBName,
				pgasserts.RoleExistsQuery("app"), "f"),
				30).Should(Succeed())
		})
	}
}

// AssertDetachReplicaModeCluster verifies that a replica cluster can be detached from the
// source cluster, and its target primary can be promoted. As such, new write operation
// on the source cluster shouldn't be received anymore by the detached replica cluster.
// Also, make sure the bootstrap fields database and owner of the replica cluster are
// properly ignored
func AssertDetachReplicaModeCluster(
	namespace,
	srcClusterName,
	srcDatabaseName,
	replicaClusterName,
	replicaDatabaseName,
	replicaUserName,
	testTableName string,
) {
	var primaryReplicaCluster *corev1.Pod

	var referenceTime time.Time
	By("taking the reference time before the detaching", func() {
		Eventually(func(g Gomega) {
			referenceCondition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, replicaClusterName,
				apiv1.ConditionClusterReady,
			)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(referenceCondition.Status).To(BeEquivalentTo(corev1.ConditionTrue))
			g.Expect(referenceCondition).ToNot(BeNil())
			referenceTime = referenceCondition.LastTransitionTime.Time
		}, 60, 5).Should(Succeed())
	})

	By("disabling the replica mode", func() {
		Eventually(func(g Gomega) {
			_, _, err := run.Unchecked(fmt.Sprintf(
				"kubectl patch cluster %v -n %v  -p '{\"spec\":{\"replica\":{\"enabled\":false}}}'"+
					" --type='merge'",
				replicaClusterName, namespace,
			))
			g.Expect(err).ToNot(HaveOccurred())
		}, 60, 5).Should(Succeed())
	})

	By("ensuring the replica cluster got promoted and restarted", func() {
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			condition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, cluster.Name,
				apiv1.ConditionClusterReady,
			)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(condition).ToNot(BeNil())
			g.Expect(condition.Status).To(BeEquivalentTo(corev1.ConditionTrue))
			g.Expect(condition.LastTransitionTime.Time).To(BeTemporally(">", referenceTime))
		}).WithTimeout(60 * time.Second).Should(Succeed())
		clusterasserts.AssertClusterIsReady(env, namespace, replicaClusterName, testTimeouts[timeouts.ClusterIsReady])
	})

	By("verifying write operation on the replica cluster primary pod", func() {
		query := "CREATE TABLE IF NOT EXISTS replica_cluster_primary AS VALUES (1),(2);"
		// Expect write operation to succeed
		Eventually(func(g Gomega) {
			var err error

			// Get primary from replica cluster
			primaryReplicaCluster, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
				replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			_, _, err = exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryReplicaCluster.Namespace,
					PodName:   primaryReplicaCluster.Name,
				}, exec.DatabaseName(srcDatabaseName),
				query,
			)
			g.Expect(err).ToNot(HaveOccurred())
		}, 300, 15).Should(Succeed())
	})

	By("verifying the replica database doesn't exist in the replica cluster", func() {
		// Application database configuration is skipped for replica clusters,
		// so we expect these to not be present
		Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, postgres.PostgresDBName,
			pgasserts.DatabaseExistsQuery(replicaDatabaseName), "f"),
			30).Should(Succeed())
		Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, postgres.PostgresDBName,
			pgasserts.RoleExistsQuery(replicaUserName), "f"),
			30).Should(Succeed())
	})

	By("writing some new data to the source cluster", func() {
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  srcClusterName,
			DatabaseName: srcDatabaseName,
			TableName:    testTableName,
		}
		pgasserts.AssertCreateTestData(env, tableLocator)
	})

	By("verifying that replica cluster was not modified", func() {
		outTables, stdErr, err := exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: primaryReplicaCluster.Namespace,
				PodName:   primaryReplicaCluster.Name,
			}, exec.DatabaseName(srcDatabaseName),
			"\\dt",
			RetryTimeout,
			PollingTime,
		)
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v\n", outTables, stdErr)
		}
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Contains(outTables, testTableName), err).Should(BeFalse())
	})
}

func AssertFastFailOver(
	namespace,
	sampleFile,
	clusterName,
	webTestFile,
	webTestJob string,
	maxReattachTime,
	maxFailoverTime int32,
) {
	var err error
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		timeout := 20
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}

		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err = env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, timeout).Should(BeEquivalentTo(namespace))
	})

	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		resources.CreateResourceFromFile(env, namespace, sampleFile)
	})

	By("having a Cluster with three instances ready", func() {
		clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
	})

	// Node 1 should be the primary, so the -rw service should
	// point there. We verify this.
	By("having the current primary on node1", func() {
		rwServiceName := clusterName + "-rw"
		endpointSlice, err := testsUtils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, rwServiceName)
		Expect(err).ToNot(HaveOccurred())

		pod := &corev1.Pod{}
		podName := clusterName + "-1"
		err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(testsUtils.FirstEndpointSliceIP(endpointSlice)).To(BeEquivalentTo(pod.Status.PodIP))
	})

	By("preparing the db for the test scenario", func() {
		// Create the table used by the scenario
		query := "CREATE SCHEMA IF NOT EXISTS tps; " +
			"CREATE TABLE IF NOT EXISTS tps.tl ( " +
			"id BIGSERIAL" +
			", timeline TEXT DEFAULT (substring(pg_walfile_name(" +
			"    pg_current_wal_lsn()), 1, 8))" +
			", t timestamp DEFAULT (clock_timestamp() AT TIME ZONE 'UTC')" +
			", source text NOT NULL" +
			", PRIMARY KEY (id)" +
			")"

		_, err = postgres.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix, query,
		)
		Expect(err).ToNot(HaveOccurred())
	})

	By("starting load", func() {
		// We set up Apache Benchmark and webtest. Apache Benchmark, a load generator,
		// continuously calls the webtest api to execute inserts
		// on the postgres primary. We make sure that the first
		// records appear on the database before moving to the next
		// step.
		_, _, err = run.Run("kubectl create -n " + namespace +
			" -f " + webTestFile)
		Expect(err).ToNot(HaveOccurred())

		webtestDeploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "webtest", Namespace: namespace}}
		Expect(deployments.WaitForReady(env.Ctx, env.Client, webtestDeploy, 60)).To(Succeed())

		_, _, err = run.Run("kubectl create -n " + namespace +
			" -f " + webTestJob)
		Expect(err).ToNot(HaveOccurred())

		primaryPodName := clusterName + "-1"
		primaryPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      primaryPodName,
		}

		query := "SELECT count(*) > 0 FROM tps.tl"
		Eventually(func() (string, error) {
			primaryPod := &corev1.Pod{}
			if err = env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod); err != nil {
				return "", err
			}
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				postgres.AppDBName,
				query,
			)
			return strings.TrimSpace(out), err
		}, RetryTimeout).Should(BeEquivalentTo("t"))
	})

	By("deleting the primary", func() {
		// The primary is force-deleted.
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}
		lm := clusterName + "-1"
		err = podutils.Delete(env.Ctx, env.Client, namespace, lm, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertStandbysFollowPromotion(namespace, clusterName, maxReattachTime)

	AssertWritesResumedBeforeTimeout(namespace, clusterName, maxFailoverTime)
}

func AssertCustomMetricsResourcesExist(namespace, sampleFile string, configMapsCount, secretsCount int) {
	By("verifying the custom metrics ConfigMaps and Secrets exist", func() {
		// Create the ConfigMaps and a Secret
		_, _, err := run.Run("kubectl apply -n " + namespace + " -f " + sampleFile)
		Expect(err).ToNot(HaveOccurred())

		// Check configmaps exist
		timeout := 20
		Eventually(func() ([]corev1.ConfigMap, error) {
			cmList := &corev1.ConfigMapList{}
			err = env.Client.List(
				env.Ctx, cmList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"e2e": "metrics"},
			)
			return cmList.Items, err
		}, timeout).Should(HaveLen(configMapsCount))

		// Check secret exists
		Eventually(func() ([]corev1.Secret, error) {
			secretList := &corev1.SecretList{}
			err = env.Client.List(
				env.Ctx, secretList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"e2e": "metrics"},
			)
			return secretList.Items, err
		}, timeout).Should(HaveLen(secretsCount))
	})
}

func AssertMetricsData(namespace, targetOne, targetTwo, targetSecret string, cluster *apiv1.Cluster) {
	By("collect and verify metric being exposed with target databases", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			var out string
			var err error
			Eventually(func(g Gomega) {
				out, err = proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, pod, cluster.IsMetricsTLSEnabled())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(strings.Contains(out,
					fmt.Sprintf(`cnpg_some_query_rows{datname="%v"} 0`, targetOne))).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
				g.Expect(strings.Contains(out,
					fmt.Sprintf(`cnpg_some_query_rows{datname="%v"} 0`, targetTwo))).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
				g.Expect(strings.Contains(out, fmt.Sprintf(`cnpg_some_query_test_rows{datname="%v"} 1`,
					targetSecret))).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			}, testTimeouts[timeouts.Short]).To(Succeed())

			if pod.Name != cluster.Status.CurrentPrimary {
				continue
			}
			Expect(out).Should(ContainSubstring("last_available_backup_timestamp"))
			Expect(out).Should(ContainSubstring("last_failed_backup_timestamp"))
		}
	})
}

func CreateAndAssertServerCertificatesSecrets(
	namespace, clusterName, caSecName, tlsSecName string, includeCAPrivateKey bool,
) {
	cluster, caPair, err := secrets.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, clusterName, caSecName, includeCAPrivateKey,
	)
	Expect(err).ToNot(HaveOccurred())

	altDNSNames := cluster.GetClusterAltDNSNames()
	// Required to allow connecting via port-forwarding using "localhost" as the host
	altDNSNames = append(altDNSNames, "localhost")

	serverPair, err := caPair.CreateAndSignPair(cluster.GetServiceReadWriteName(), certs.CertTypeServer, altDNSNames)
	Expect(err).ToNot(HaveOccurred())
	serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

func CreateAndAssertClientCertificatesSecrets(
	namespace, clusterName, caSecName, tlsSecName, userSecName string, includeCAPrivateKey bool,
) {
	_, caPair, err := secrets.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, clusterName, caSecName, includeCAPrivateKey,
	)
	Expect(err).ToNot(HaveOccurred())

	// Sign tls certificates for streaming_replica user
	serverPair, err := caPair.CreateAndSignPair("streaming_replica", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())

	// Creating 'app' user tls certificates to validate connection from psql client
	serverPair, err = caPair.CreateAndSignPair("app", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	serverSecret = serverPair.GenerateCertificateSecret(namespace, userSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

func AssertSSLVerifyFullDBConnectionFromAppPod(namespace string, clusterName string, appPod corev1.Pod) {
	By("creating an app Pod and connecting to DB, using Certificate authentication", func() {
		// Connecting to DB, using Certificate authentication
		Eventually(func() (string, string, error) {
			dsn := fmt.Sprintf("host=%v-rw.%v.svc port=5432 "+
				"sslkey=/etc/secrets/tls/tls.key "+
				"sslcert=/etc/secrets/tls/tls.crt "+
				"sslrootcert=/etc/secrets/ca/ca.crt "+
				"dbname=app user=app sslmode=verify-full", clusterName, namespace)
			timeout := time.Second * 10
			stdout, stderr, err := exec.Command(
				env.Ctx, env.Interface, env.RestClientConfig,
				appPod, appPod.Spec.Containers[0].Name, &timeout,
				"psql", dsn, "-tAc", "SELECT 1",
			)
			return stdout, stderr, err
		}, 360).Should(BeEquivalentTo("1\n"))
	})
}

func AssertClusterRestoreWithApplicationDB(namespace, restoreClusterFile, tableName string) {
	restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		resources.CreateResourceFromFile(env, namespace, restoreClusterFile)

		// We give more time than the usual 600s, since the recovery is slower
		clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

		// Test data should be present on restored primary
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  restoredClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
	})

	By("Ensuring the restored cluster is on timeline 2", func() {
		row, err := postgres.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			restoredClusterName,
			postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"SELECT substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var timeline string
		err = row.Scan(&timeline)
		Expect(err).ToNot(HaveOccurred())
		Expect(timeline).To(BeEquivalentTo("00000002"))
	})

	// Restored standby should be attached to restored primary
	AssertClusterStandbysAreStreaming(namespace, restoredClusterName, 140)

	// Gather Credentials
	appUser, appUserPass, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		restoredClusterName, namespace,
		apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())
	secretName := restoredClusterName + apiv1.ApplicationUserSecretSuffix

	By("checking the restored cluster with pre-defined app password connectable", func() {
		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			restoredClusterName,
			appUser,
			postgres.AppDBName,
			appUserPass,
			secretName)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx"
		AssertUpdateSecret("password", newPassword, secretName, namespace, restoredClusterName, 30, env)

		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			restoredClusterName,
			appUser,
			postgres.AppDBName,
			newPassword,
			secretName)
	})
}

func AssertClusterRestore(namespace, restoreClusterFile, tableName string) {
	restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		resources.CreateResourceFromFile(env, namespace, restoreClusterFile)

		// We give more time than the usual 600s, since the recovery is slower
		clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

		// Test data should be present on restored primary
		primary := restoredClusterName + "-1"
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  restoredClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)

		// Restored primary should be on timeline 2
		out, _, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: namespace,
				PodName:   primary,
			},
			postgres.AppDBName,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

		// Restored standby should be attached to restored primary
		AssertClusterStandbysAreStreaming(namespace, restoredClusterName, 140)
	})
}

// AssertClusterImport imports a database into a new cluster, and verifies that
// the new cluster is functioning properly
func AssertClusterImport(namespace, clusterWithExternalClusterName, clusterName, databaseName string) *apiv1.Cluster {
	var cluster *apiv1.Cluster
	By("Importing Database in a new cluster", func() {
		var err error
		cluster, err = importdb.ImportDatabaseMicroservice(env.Ctx, env.Client, namespace, clusterName,
			clusterWithExternalClusterName, "", databaseName)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		clusterasserts.AssertClusterIsReady(env, namespace, clusterWithExternalClusterName,
			testTimeouts[timeouts.ClusterIsReadySlow])

		// Restored standby should be attached to restored primary
		AssertClusterStandbysAreStreaming(namespace, clusterWithExternalClusterName, 140)
	})
	return cluster
}

func AssertClusterWasRestoredWithPITRAndApplicationDB(namespace, clusterName, tableName, lsn string) {
	// We give more time than the usual 600s, since the recovery is slower
	clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReadySlow])

	// Gather the recovered cluster primary
	primaryInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	secretName := clusterName + apiv1.ApplicationUserSecretSuffix

	By("Ensuring the restored cluster is on timeline 3", func() {
		// Restored primary should be on timeline 3
		row, err := postgres.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		// Restored standby should be attached to restored primary
		Expect(postgres.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			primaryInfo, RetryTimeout,
		)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		// Only 2 entries should be present
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  clusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
	})

	// Gather credentials
	appUser, appUserPass, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())

	By("checking the restored cluster with auto generated app password connectable", func() {
		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			clusterName,
			appUser,
			postgres.AppDBName,
			appUserPass,
			secretName)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx"
		AssertUpdateSecret("password", newPassword, secretName, namespace, clusterName, 30, env)
		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			clusterName,
			appUser,
			postgres.AppDBName,
			newPassword,
			secretName)
	})
}

func collectAndAssertDefaultMetricsPresentOnEachPod(
	namespace, clusterName string,
	tlsEnabled bool,
	expectPresent bool,
) {
	By("collecting and verifying a set of default metrics on each pod", func() {
		defaultMetrics := []string{
			"cnpg_pg_settings_setting",
			"cnpg_backends_waiting_total",
			"cnpg_pg_postmaster_start_time",
			"cnpg_pg_replication",
			"cnpg_pg_stat_bgwriter",
			"cnpg_pg_stat_database",
		}

		if env.PostgresVersion > 16 {
			defaultMetrics = append(
				defaultMetrics,
				"cnpg_pg_stat_checkpointer",
			)
		}

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			Eventually(func(g Gomega) {
				out, err := proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, pod, tlsEnabled)
				g.Expect(err).ToNot(HaveOccurred())

				// error should be zero on each pod metrics
				g.Expect(strings.Contains(out, "cnpg_collector_last_collection_error 0")).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
				// verify that, default set of monitoring queries should not be existed on each pod
				for _, data := range defaultMetrics {
					if expectPresent {
						g.Expect(strings.Contains(out, data)).Should(BeTrue(),
							"Metric collection issues on pod %v."+
								"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
					} else {
						g.Expect(strings.Contains(out, data)).Should(BeFalse(),
							"Metric collection issues on pod %v."+
								"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
					}
				}
			}, testTimeouts[timeouts.Short]).Should(Succeed())
		}
	})
}

// collectAndAssertMetricsPresentOnEachPod verify a set of metrics is existed in each pod
func collectAndAssertCollectorMetricsPresentOnEachPod(cluster *apiv1.Cluster) {
	cnpgCollectorMetrics := []string{
		"cnpg_collector_collection_duration_seconds",
		"cnpg_collector_fencing_on",
		"cnpg_collector_nodes_used",
		"cnpg_collector_pg_wal",
		"cnpg_collector_pg_wal_archive_status",
		"cnpg_collector_postgres_version",
		"cnpg_collector_collections_total",
		"cnpg_collector_last_collection_error",
		"cnpg_collector_collection_duration_seconds",
		"cnpg_collector_manual_switchover_required",
		"cnpg_collector_sync_replicas",
		"cnpg_collector_replica_mode",
	}

	if env.PostgresVersion >= 14 {
		cnpgCollectorMetrics = append(
			cnpgCollectorMetrics,
			"cnpg_collector_wal_records",
			"cnpg_collector_wal_fpi",
			"cnpg_collector_wal_bytes",
			"cnpg_collector_wal_buffers_full",
		)
		if env.PostgresVersion < 18 {
			cnpgCollectorMetrics = append(
				cnpgCollectorMetrics,
				"cnpg_collector_wal_write",
				"cnpg_collector_wal_sync",
				"cnpg_collector_wal_write_time",
				"cnpg_collector_wal_sync_time",
			)
		}
	}
	By("collecting and verify set of collector metrics on each pod", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			Eventually(func(g Gomega) {
				out, err := proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, pod, cluster.IsMetricsTLSEnabled())
				g.Expect(err).ToNot(HaveOccurred())

				// error should be zero on each pod metrics
				g.Expect(strings.Contains(out, "cnpg_collector_last_collection_error 0")).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
				// verify that, default set of monitoring queries should not be existed on each pod
				for _, data := range cnpgCollectorMetrics {
					g.Expect(strings.Contains(out, data)).Should(BeTrue(),
						"Metric collection issues on pod %v."+
							"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
				}
			}, testTimeouts[timeouts.Short]).To(Succeed())
		}
	})
}

// CreateResourcesFromFileWithError creates the Kubernetes objects defined in the
// YAML sample file and returns any errors
// AssertPvcHasLabels verifies if the PVCs of a cluster in a given namespace
// contains the expected labels, and their values reflect the current status
// of the related pods.
func AssertPvcHasLabels(
	namespace,
	clusterName string,
) {
	By("checking PVC have the correct role labels", func() {
		Eventually(func(g Gomega) {
			// Gather the list of PVCs in the current namespace
			pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
			g.Expect(err).ToNot(HaveOccurred())

			// Iterating through PVC list
			for _, pvc := range pvcList.Items {
				// Gather the podName related to the current pvc using nodeSerial
				podName := fmt.Sprintf("%v-%v", clusterName, pvc.Annotations[utils.ClusterSerialAnnotationName])
				pod := &corev1.Pod{}
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				err = env.Client.Get(env.Ctx, podNamespacedName, pod)
				g.Expect(err).ToNot(HaveOccurred())

				ExpectedRole := "replica"
				if specs.IsPodPrimary(*pod) {
					ExpectedRole = "primary"
				}
				ExpectedPvcRole := "PG_DATA"
				if pvc.Name == podName+"-wal" {
					ExpectedPvcRole = "PG_WAL"
				}
				expectedLabels := map[string]string{
					utils.ClusterLabelName:             clusterName,
					utils.PvcRoleLabelName:             ExpectedPvcRole,
					utils.ClusterInstanceRoleLabelName: ExpectedRole,
				}
				g.Expect(storage.PvcHasLabels(pvc, expectedLabels)).To(BeTrue(),
					fmt.Sprintf("expectedLabels: %v and found actualLabels on pvc: %v",
						expectedLabels, pod.GetLabels()))
			}
		}, 300, 5).Should(Succeed())
	})
}

// AssertReplicationSlotsOnPod checks that all the required replication slots exist in a given pod,
// and that obsolete slots are correctly deleted (post management operations).
// In the primary, it will also check if the slots are active.
func AssertReplicationSlotsOnPod(
	namespace,
	clusterName string,
	pod corev1.Pod,
	expectedSlots []string,
	isActiveOnPrimary bool,
	isActiveOnReplica bool,
) {
	GinkgoWriter.Println("Checking slots presence:", expectedSlots, "in pod:", pod.Name)
	Eventually(func() ([]string, error) {
		currentSlots, err := replicationslot.GetReplicationSlotsOnPod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, pod.GetName(), postgres.AppDBName,
		)
		return currentSlots, err
	}, 300).Should(ContainElements(expectedSlots),
		func() string {
			return replicationslot.PrintReplicationSlots(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, postgres.AppDBName,
			)
		})

	GinkgoWriter.Println("Verifying slots status for pod", pod.Name)

	for _, slot := range expectedSlots {
		query := fmt.Sprintf(
			"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots "+
				"WHERE slot_name = '%v' AND active = '%t' "+
				"AND temporary = 'f' AND slot_type = 'physical')", slot, isActiveOnReplica,
		)
		if specs.IsPodPrimary(pod) {
			query = fmt.Sprintf(
				"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots "+
					"WHERE slot_name = '%v' AND active = '%t' "+
					"AND temporary = 'f' AND slot_type = 'physical')", slot, isActiveOnPrimary,
			)
		}
		Eventually(func() (string, error) {
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				postgres.PostgresDBName,
				query,
			)
			return strings.TrimSpace(stdout), err
		}, 300).Should(BeEquivalentTo("t"),
			func() string {
				return replicationslot.PrintReplicationSlots(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName, postgres.AppDBName,
				)
			})
	}
}

// AssertClusterReplicationSlotsAligned will compare all the replication slot restart_lsn
// in a cluster. The assertion will succeed if they are all equivalent.
func AssertClusterReplicationSlotsAligned(
	namespace,
	clusterName string,
) {
	podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	Eventually(func(g Gomega) {
		numPods := len(podList.Items)
		// Capacity calculation: primary has (N-1) HA slots, each of the (N-1) replicas
		// has (N-2) slots. Total LSNs: (N-1) + (N-1)*(N-2) = (N-1)^2
		lsnList := make([]string, 0, (numPods-1)*(numPods-1))
		for _, pod := range podList.Items {
			out, err := replicationslot.GetReplicationSlotLsnsOnPod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, postgres.AppDBName, pod,
			)
			g.Expect(err).ToNot(HaveOccurred(), "error getting replication slot lsn on pod %v", pod.Name)
			lsnList = append(lsnList, out...)
		}
		g.Expect(replicationslot.AreSameLsn(lsnList)).To(BeTrue())
	}).WithTimeout(300*time.Second).WithPolling(2*time.Second).Should(Succeed(),
		func() string {
			return replicationslot.PrintReplicationSlots(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, postgres.AppDBName,
			)
		})
}

// AssertClusterHAReplicationSlots will verify if the replication slots of each pod
// of the cluster exist and are aligned.
func AssertClusterHAReplicationSlots(namespace, clusterName string) {
	By("verifying all cluster's replication slots exist and are aligned", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			expectedSlots, err := replicationslot.GetExpectedHAReplicationSlotsOnPod(
				env.Ctx, env.Client,
				namespace, clusterName, pod.GetName(),
			)
			Expect(err).ToNot(HaveOccurred())
			AssertReplicationSlotsOnPod(namespace, clusterName, pod, expectedSlots, true, false)
		}
		AssertClusterReplicationSlotsAligned(namespace, clusterName)
	})
}

// assertIncludesMetrics is a utility function used for asserting that specific metrics,
// defined by regular expressions in
// the 'expectedMetrics' map, are present in the 'rawMetricsOutput' string.
// It also checks whether the metrics match the expected format defined by their regular expressions.
// If any assertion fails, it prints an error message to GinkgoWriter.
//
// Parameters:
//   - rawMetricsOutput: The raw metrics data string to be checked.
//   - expectedMetrics: A map of metric names to regular expressions that describe the expected format of the metrics.
//
// Example usage:
//
//	expectedMetrics := map[string]*regexp.Regexp{
//	    "cpu_usage":   regexp.MustCompile(`^\d+\.\d+$`), // Example: "cpu_usage 0.25"
//	    "memory_usage": regexp.MustCompile(`^\d+\s\w+$`), // Example: "memory_usage 512 MiB"
//	}
//	assertIncludesMetrics(rawMetricsOutput, expectedMetrics)
//
// The function will assert that the specified metrics exist in 'rawMetricsOutput' and match their expected formats.
// If any assertion fails, it will print an error message with details about the failed metric collection.
//
// Note: This function is typically used in testing scenarios to validate metric collection behavior.
func assertIncludesMetrics(g Gomega, rawMetricsOutput string, expectedMetrics map[string]*regexp.Regexp) {
	debugDetails := fmt.Sprintf("Priting rawMetricsOutput:\n%s", rawMetricsOutput)
	withDebugDetails := func(baseErrMessage string) string {
		return fmt.Sprintf("%s\n%s\n", baseErrMessage, debugDetails)
	}

	for key, valueRe := range expectedMetrics {
		re := regexp.MustCompile(fmt.Sprintf("(?m)^(%s).*$", key))

		// match a metric with the value of expectedMetrics key
		match := re.FindString(rawMetricsOutput)
		g.Expect(match).NotTo(BeEmpty(), withDebugDetails(fmt.Sprintf("Found no match for metric %s", key)))

		// extract the value from the metric previously matched
		value := strings.Fields(match)[1]
		g.Expect(strings.Fields(match)[1]).NotTo(BeEmpty(),
			withDebugDetails(fmt.Sprintf("Found no result for metric %s.Metric line: %s", key, match)))

		// expect the expectedMetrics regexp to match the value of the metric
		g.Expect(valueRe.MatchString(value)).To(BeTrue(),
			withDebugDetails(fmt.Sprintf("Expected %s to have value %v but got %s", key, valueRe, value)))
	}
}

func assertExcludesMetrics(g Gomega, rawMetricsOutput string, nonCollected []string) {
	for _, nonCollectable := range nonCollected {
		// match a metric with the value of expectedMetrics key
		g.Expect(rawMetricsOutput).NotTo(ContainSubstring(nonCollectable))
	}
}
