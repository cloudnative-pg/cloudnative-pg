/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", func() {
	const (
		targetDBOne             = "test"
		targetDBTwo             = "test1"
		targetDBSecret          = "secret_test"
		testTableName           = "test_table"
		customQueriesSampleFile = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
		clusterMetricsFile      = fixturesDir + "/metrics/cluster-metrics.yaml"
	)

	// Cluster identifiers
	var namespace, metricsClusterName string

	// We define a few metrics in the tests. We check that all of them exist and
	// there are no errors during the collection.
	metricsRegexp := regexp.MustCompile(
		`(?m:^(` +
			`cnp_pg_postmaster_start_time_seconds \d+\.\d+|` + // wokeignore:rule=master
			`cnp_pg_wal_files_total \d+|` +
			`cnp_pg_database_size_bytes{datname="app"} [0-9e\+\.]+|` +
			`cnp_pg_replication_slots_inactive 0|` +
			`cnp_pg_stat_archiver_archived_count \d+|` +
			`cnp_pg_stat_archiver_failed_count \d+|` +
			`cnp_pg_locks_blocked_queries 0|` +
			`cnp_collector_last_collection_error 0)` +
			`$)`)

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterMetricsFile,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})
	It("can gather metrics", func() {
		// Create the cluster namespace
		namespace = "cluster-metrics-e2e"
		metricsClusterName = "postgresql-metrics"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		AssertCustomMetricsResourcesExist(namespace, fixturesDir+"/metrics/custom-queries.yaml", 2, 1)

		// Create the cluster
		AssertCreateCluster(namespace, metricsClusterName, clusterMetricsFile, env)

		By("collecting metrics on each pod", func() {
			podList, err := env.GetClusterPodList(namespace, metricsClusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather metrics in each pod
			for _, pod := range podList.Items {
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					pod.GetName(),
					"sh -c 'curl -s 127.0.0.1:9187/metrics'"))
				matches := metricsRegexp.FindAllString(out, -1)
				Expect(matches, err).To(HaveLen(8), "Metric collection issues on %v.\nCollected metrics:\n%v", pod.GetName(), out)
			}
		})
	})

	It("can gather metrics with multiple target databases", func() {
		namespace = "metrics-target-databases-e2e"
		metricsClusterName = "metrics-target-databases"
		ClusterSampleFile := fixturesDir + "/metrics/cluster-metrics-with-target-databases.yaml"
		// Create the cluster namespace
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCustomMetricsResourcesExist(namespace, customQueriesSampleFile, 1, 1)
		// Create the cluster
		AssertCreateCluster(namespace, metricsClusterName, ClusterSampleFile, env)
		CreateTestDataForTargetDB(namespace, metricsClusterName, targetDBOne, testTableName)
		CreateTestDataForTargetDB(namespace, metricsClusterName, targetDBTwo, testTableName)
		CreateTestDataForTargetDB(namespace, metricsClusterName, targetDBSecret, testTableName)
		AssertMetricsData(namespace, metricsClusterName, targetDBOne, targetDBTwo, targetDBSecret)
	})
})

func AssertCustomMetricsResourcesExist(namespace, sampleFile string, configMapsCount, secretsCount int) {
	By("verifying the custom metrics ConfigMaps and Secrets exist", func() {
		// Create the ConfigMaps and a Secret
		_, _, err := tests.Run("kubectl apply -n " + namespace + " -f " + sampleFile)
		Expect(err).ToNot(HaveOccurred())

		// Check configmaps exist
		timeout := 20
		Eventually(func() ([]corev1.ConfigMap, error) {
			cmList := &corev1.ConfigMapList{}
			err := env.Client.List(
				env.Ctx, cmList, client.InNamespace(namespace),
				client.MatchingLabels{"e2e": "metrics"},
			)
			return cmList.Items, err
		}, timeout).Should(HaveLen(configMapsCount))

		// Check secret exists
		Eventually(func() ([]corev1.Secret, error) {
			secretList := &corev1.SecretList{}
			err := env.Client.List(
				env.Ctx, secretList, client.InNamespace(namespace),
				client.MatchingLabels{"e2e": "metrics"},
			)
			return secretList.Items, err
		}, timeout).Should(HaveLen(secretsCount))
	})
}

func CreateTestDataForTargetDB(namespace, clusterName, targetDBName, tableName string) {
	By(fmt.Sprintf("creating target database '%v' and table '%v'", targetDBName, tableName), func() {
		primaryPodName, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		timeout := time.Second * 2
		// Create database
		createDBQuery := fmt.Sprintf("create database %v;", targetDBName)
		_, _, err = env.ExecCommand(env.Ctx, *primaryPodName, "postgres", &timeout,
			"psql", "-U", "postgres", "-tAc", createDBQuery)
		Expect(err).ToNot(HaveOccurred())
		// Create table on target database
		dsn := fmt.Sprintf("user=postgres port=5432 dbname=%v ", targetDBName)
		createTableQuery := fmt.Sprintf("create table %v (id int);", tableName)
		_, _, err = env.ExecCommand(env.Ctx, *primaryPodName, "postgres", &timeout,
			"psql", dsn, "-tAc", createTableQuery)
		Expect(err).ToNot(HaveOccurred())
		// Grant a permission
		grantRoleQuery := "GRANT SELECT ON all tables in schema public to pg_monitor;"
		_, _, err = env.ExecCommand(env.Ctx, *primaryPodName, "postgres", &timeout,
			"psql", "-U", "postgres", dsn, "-tAc", grantRoleQuery)
		Expect(err).ToNot(HaveOccurred())
	})
}

func AssertMetricsData(namespace, clusterName, targetOne, targetTwo, targetSecret string) {
	By("collect and verify metric being exposed with target databases", func() {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				podName,
				"sh -c 'curl -s 127.0.0.1:9187/metrics'"))
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(out, fmt.Sprintf(`cnp_some_query_rows{datname="%v"} 0`, targetOne))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			Expect(strings.Contains(out, fmt.Sprintf(`cnp_some_query_rows{datname="%v"} 0`, targetTwo))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			Expect(strings.Contains(out, fmt.Sprintf(`cnp_some_query_test_rows{datname="%v"} 1`,
				targetSecret))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
		}
	})
}
