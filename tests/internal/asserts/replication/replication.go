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

// Package replication provides Ginkgo/Gomega assertions for streaming
// replication state: read-only / read-write service behaviour, replica
// promotion and lag, replication slot accounting, replica clusters,
// and the fast-failover end-to-end choreography.
package replication

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	testsutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertWritesToReplicaFails opens a connection to the named service and
// expects it to land on a replica (recovery=true) where DDL is rejected.
func AssertWritesToReplicaFails(
	env *environment.TestingEnvironment,
	namespace, service, appDBName, appDBUser, appDBPass string,
	connectionParams ...map[string]string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("Verifying %v service doesn't allow writes", service), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := pgutils.ForwardPSQLServiceConnection(
				env.Ctx,
				env.Interface,
				env.RestClientConfig,
				namespace,
				service,
				appDBName,
				appDBUser,
				appDBPass,
				connectionParams...,
			)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			// Expect to be connected to a replica
			row := conn.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			isReplica := strings.TrimSpace(rawValue)
			g.Expect(isReplica).To(BeEquivalentTo("true"))

			// Expect to be in a read-only transaction
			_, err = conn.Exec("CREATE TABLE IF NOT EXISTS table1(var1 text)")
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("cannot execute CREATE TABLE in a read-only transaction"))
		}, environment.RetryTimeout).Should(Succeed())
	})
}

// AssertWritesToPrimarySucceeds opens a connection to the named service
// and expects it to land on the primary, where DDL succeeds.
func AssertWritesToPrimarySucceeds(
	env *environment.TestingEnvironment,
	namespace, service, appDBName, appDBUser, appDBPass string,
	connectionParams ...map[string]string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("Verifying %v service correctly manages writes", service), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := pgutils.ForwardPSQLServiceConnection(
				env.Ctx,
				env.Interface,
				env.RestClientConfig,
				namespace,
				service,
				appDBName,
				appDBUser,
				appDBPass,
				connectionParams...,
			)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			// Expect to be connected to a primary
			row := conn.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			isReplica := strings.TrimSpace(rawValue)
			g.Expect(isReplica).To(BeEquivalentTo("false"))

			// Expect to be able to write
			_, err = conn.Exec("CREATE TABLE IF NOT EXISTS table1(var1 text)")
			g.Expect(err).ToNot(HaveOccurred())
		}, environment.RetryTimeout).Should(Succeed())
	})
}

// AssertClusterStandbysAreStreaming verifies that all the standbys of a
// cluster have a wal-receiver running.
func AssertClusterStandbysAreStreaming(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	timeout int,
) {
	GinkgoHelper()
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
				pgutils.PostgresDBName,
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

// AssertStandbysFollowPromotion verifies every cluster pod observes
// timeline 2 after a primary promotion, and that the cluster reaches
// Ready before the deadline.
func AssertStandbysFollowPromotion(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName string,
	timeout int,
) {
	GinkgoHelper()
	start := time.Now()

	By(fmt.Sprintf("having all the instances on timeline 2 in less than %v sec", timeout), func() {
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
					pgutils.AppDBName,
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
		GinkgoWriter.Printf("Cluster has been in a degraded state for %v seconds\n", elapsed)
		Expect(elapsed.Seconds()).To(BeNumerically("<", timeout))
	})
}

// AssertWritesResumedBeforeTimeout measures the gap in seconds between the
// last write on the old timeline and the first write on the new timeline,
// asserting it is below timeout.
func AssertWritesResumedBeforeTimeout(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	timeout int,
) {
	GinkgoHelper()
	By(fmt.Sprintf("resuming writing in less than %v sec", timeout), func() {
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
			}, pgutils.AppDBName,
			query,
			environment.RetryTimeout,
			objects.PollingTime,
		)
		Expect(err).ToNot(HaveOccurred())
		switchTime, err = strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err == nil {
			GinkgoWriter.Printf("Write activity resumed in %v seconds\n", switchTime)
		}
		Expect(switchTime, err).Should(BeNumerically("<", timeout))
	})
}

// AssertReplicaModeCluster checks that, after inserting some data in a
// source cluster, a replica cluster can be bootstrapped using
// pg_basebackup and is properly replicating from the source cluster.
func AssertReplicaModeCluster(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, srcClusterName, srcClusterDBName, replicaClusterSample, testTableName string,
) {
	GinkgoHelper()
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
		forwardSource, connSource, err := pgutils.ForwardPSQLConnection(
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
			Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, pgutils.PostgresDBName,
				pgasserts.DatabaseExistsQuery("app"), "f"),
				30).Should(Succeed())
			Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, pgutils.PostgresDBName,
				pgasserts.RoleExistsQuery("app"), "f"),
				30).Should(Succeed())
		})
	}
}

// AssertDetachReplicaModeCluster verifies that a replica cluster can be
// detached from the source cluster, and its target primary can be
// promoted. After detachment, new writes on the source cluster shouldn't
// reach the detached replica cluster, and the replica cluster must not
// have a bootstrap.initdb-style "app" database.
func AssertDetachReplicaModeCluster(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, srcClusterName, srcDatabaseName string,
	replicaClusterName, replicaDatabaseName, replicaUserName string,
	testTableName string,
) {
	GinkgoHelper()
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
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			original := cluster.DeepCopy()
			cluster.Spec.ReplicaCluster.Enabled = ptr.To(false)
			g.Expect(env.Client.Patch(env.Ctx, cluster, ctrlclient.MergeFrom(original))).To(Succeed())
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
		Eventually(func(g Gomega) {
			var err error
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
		Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, pgutils.PostgresDBName,
			pgasserts.DatabaseExistsQuery(replicaDatabaseName), "f"),
			30).Should(Succeed())
		Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryReplicaCluster, pgutils.PostgresDBName,
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
			environment.RetryTimeout,
			objects.PollingTime,
		)
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v\n", outTables, stdErr)
		}
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Contains(outTables, testTableName), err).Should(BeFalse())
	})
}

// AssertFastFailOver creates a cluster, drives load against it via a
// webtest deployment, force-deletes the primary, and verifies that
// standbys follow promotion and writes resume before the deadlines.
func AssertFastFailOver(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, sampleFile, clusterName, webTestFile, webTestJob string,
	maxReattachTime, maxFailoverTime int,
	quickDeletionPeriod int64,
) {
	GinkgoHelper()
	var err error
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
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

	By("having the current primary on node1", func() {
		rwServiceName := clusterName + "-rw"
		endpointSlice, err := testsutils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, rwServiceName)
		Expect(err).ToNot(HaveOccurred())

		pod := &corev1.Pod{}
		podName := clusterName + "-1"
		err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(testsutils.FirstEndpointSliceIP(endpointSlice)).To(BeEquivalentTo(pod.Status.PodIP))
	})

	By("preparing the db for the test scenario", func() {
		query := "CREATE SCHEMA IF NOT EXISTS tps; " +
			"CREATE TABLE IF NOT EXISTS tps.tl ( " +
			"id BIGSERIAL" +
			", timeline TEXT DEFAULT (substring(pg_walfile_name(" +
			"    pg_current_wal_lsn()), 1, 8))" +
			", t timestamp DEFAULT (clock_timestamp() AT TIME ZONE 'UTC')" +
			", source text NOT NULL" +
			", PRIMARY KEY (id)" +
			")"

		_, err = pgutils.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, pgutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix, query,
		)
		Expect(err).ToNot(HaveOccurred())
	})

	By("starting load", func() {
		resources.CreateResourceFromFile(env, namespace, webTestFile)

		webtestDeploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "webtest", Namespace: namespace}}
		Expect(deployments.WaitForReady(env.Ctx, env.Client, webtestDeploy, 60)).To(Succeed())

		resources.CreateResourceFromFile(env, namespace, webTestJob)

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
				pgutils.AppDBName,
				query,
			)
			return strings.TrimSpace(out), err
		}, environment.RetryTimeout).Should(BeEquivalentTo("t"))
	})

	By("deleting the primary", func() {
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}
		lm := clusterName + "-1"
		err = podutils.Delete(env.Ctx, env.Client, namespace, lm, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertStandbysFollowPromotion(env, testTimeouts, namespace, clusterName, maxReattachTime)

	AssertWritesResumedBeforeTimeout(env, namespace, clusterName, maxFailoverTime)
}

// AssertReplicationSlotsOnPod checks that all the required replication
// slots exist in a given pod, and that obsolete slots are correctly
// deleted (post management operations). On the primary it also verifies
// the slots are active.
func AssertReplicationSlotsOnPod(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	pod corev1.Pod,
	expectedSlots []string,
	isActiveOnPrimary bool,
	isActiveOnReplica bool,
) {
	GinkgoHelper()
	GinkgoWriter.Println("Checking slots presence:", expectedSlots, "in pod:", pod.Name)
	Eventually(func() ([]string, error) {
		currentSlots, err := replicationslot.GetReplicationSlotsOnPod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, pod.GetName(), pgutils.AppDBName,
		)
		return currentSlots, err
	}, 300).Should(ContainElements(expectedSlots),
		func() string {
			return replicationslot.PrintReplicationSlots(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, pgutils.AppDBName,
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
				pgutils.PostgresDBName,
				query,
			)
			return strings.TrimSpace(stdout), err
		}, 300).Should(BeEquivalentTo("t"),
			func() string {
				return replicationslot.PrintReplicationSlots(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName, pgutils.AppDBName,
				)
			})
	}
}

// AssertClusterReplicationSlotsAligned compares the replication slot
// restart_lsn across the cluster; the assertion succeeds if all values
// match.
func AssertClusterReplicationSlotsAligned(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
) {
	GinkgoHelper()
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
				namespace, clusterName, pgutils.AppDBName, pod,
			)
			g.Expect(err).ToNot(HaveOccurred(), "error getting replication slot lsn on pod %v", pod.Name)
			lsnList = append(lsnList, out...)
		}
		g.Expect(replicationslot.AreSameLsn(lsnList)).To(BeTrue())
	}).WithTimeout(300*time.Second).WithPolling(2*time.Second).Should(Succeed(),
		func() string {
			return replicationslot.PrintReplicationSlots(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, pgutils.AppDBName,
			)
		})
}

// AssertClusterHAReplicationSlots verifies that the replication slots of
// each pod of the cluster exist and are aligned.
func AssertClusterHAReplicationSlots(env *environment.TestingEnvironment, namespace, clusterName string) {
	GinkgoHelper()
	By("verifying all cluster's replication slots exist and are aligned", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			expectedSlots, err := replicationslot.GetExpectedHAReplicationSlotsOnPod(
				env.Ctx, env.Client,
				namespace, clusterName, pod.GetName(),
			)
			Expect(err).ToNot(HaveOccurred())
			AssertReplicationSlotsOnPod(env, namespace, clusterName, pod, expectedSlots, true, false)
		}
		AssertClusterReplicationSlotsAligned(env, namespace, clusterName)
	})
}
