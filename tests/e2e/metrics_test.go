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
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", Label(tests.LabelObservability), func() {
	const (
		targetDBOne                    = "test"
		targetDBTwo                    = "test1"
		targetDBSecret                 = "secret_test"
		testTableName                  = "test_table"
		clusterMetricsFile             = fixturesDir + "/metrics/cluster-metrics.yaml.template"
		clusterMetricsDBFile           = fixturesDir + "/metrics/cluster-metrics-with-target-databases.yaml.template"
		customQueriesSampleFile        = fixturesDir + "/metrics/custom-queries-with-target-databases.yaml"
		defaultMonitoringConfigMapName = "cnpg-default-monitoring"
		level                          = tests.Low
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// Cluster identifiers
	var namespace, metricsClusterName, curlPodName string
	var err error

	// We define a few metrics in the tests. We check that all of them exist and
	// there are no errors during the collection.
	metricsMap := map[string]*regexp.Regexp{
		"cnpg_pg_postmaster_start_time_seconds":        regexp.MustCompile(`\d+\.\d+`), // wokeignore:rule=master
		"cnpg_pg_wal_files_total":                      regexp.MustCompile(`\d+`),
		"cnpg_pg_database_size_bytes{datname=\"app\"}": regexp.MustCompile(`[0-9e+.]+`),
		"cnpg_pg_stat_archiver_archived_count":         regexp.MustCompile(`\d+`),
		"cnpg_pg_stat_archiver_failed_count":           regexp.MustCompile(`\d+`),
		"cnpg_pg_locks_blocked_queries":                regexp.MustCompile(`0`),
		"cnpg_runonserver_match_fixed":                 regexp.MustCompile(`42`),
		"cnpg_collector_last_collection_error":         regexp.MustCompile(`0`),
	}

	AssertMetrics := func(metrics string, expectedMetrics map[string]*regexp.Regexp, podName string) {
		collectionError := fmt.Sprintf(
			"\nMetric collection issues on %v.\nPrinting metrics:\n%v\n",
			podName,
			metrics,
		)

		for key, valueRe := range expectedMetrics {
			re := regexp.MustCompile(fmt.Sprintf(`(?m)^(` + key + `).*$`))

			// match a metric with the value of expectedMetrics key
			match := re.FindString(metrics)
			if match == "" {
				_, _ = fmt.Fprint(GinkgoWriter, collectionError)
			}
			Expect(match).NotTo(BeEmpty(),
				"\nFound no match for metric %v\n", key)

			// extract the value from the metric previously matched
			value := strings.Fields(match)[1]
			if value == "" {
				_, _ = fmt.Fprint(GinkgoWriter, collectionError)
			}
			Expect(value).NotTo(BeEmpty(),
				"\nFound no result for metric %v.\nMetric line: %v\n", key, match)

			// expect the expectedMetrics regexp to match the value of the metric
			result := valueRe.MatchString(value)
			if result != true {
				_, _ = fmt.Fprint(GinkgoWriter, collectionError)
			}
			Expect(result).To(BeTrue(),
				"\nExpected %v to have value %v but got %v\n", key, valueRe, value)
		}
	}

	buildMetrics := func(
		expectedMetrics map[string]*regexp.Regexp,
		namespace,
		clusterName string,
		pod corev1.Pod,
	) (map[string]*regexp.Regexp, error) {
		cluster, err := env.GetCluster(namespace, clusterName)
		if err != nil {
			return nil, err
		}

		inactiveSlots := cluster.Spec.Instances - 2

		if specs.IsPodPrimary(pod) {
			expectedMetrics["cnpg_pg_replication_slots_status_inactive"] = regexp.MustCompile("0")
		} else {
			expectedMetrics["cnpg_pg_replication_slots_status_inactive"] = regexp.MustCompile(strconv.Itoa(inactiveSlots))
		}

		return expectedMetrics, nil
	}

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	It("can gather metrics", func() {
		// Create the cluster namespace
		const namespacePrefix = "cluster-metrics-e2e"
		metricsClusterName, err = env.GetResourceNameFromYAML(clusterMetricsFile)
		Expect(err).ToNot(HaveOccurred())
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		AssertCustomMetricsResourcesExist(namespace, fixturesDir+"/metrics/custom-queries.yaml", 2, 1)

		// Create the curl client pod and wait for it to be ready.
		By("setting up curl client pod", func() {
			curlClient := utils.CurlClient(namespace)
			err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
			Expect(err).ToNot(HaveOccurred())
			curlPodName = curlClient.GetName()
		})

		// Create the cluster
		AssertCreateCluster(namespace, metricsClusterName, clusterMetricsFile, env)

		By("collecting metrics on each pod", func() {
			podList, err := env.GetClusterPodList(namespace, metricsClusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather metrics in each pod
			for _, pod := range podList.Items {
				podIP := pod.Status.PodIP
				out, err := utils.CurlGetMetrics(namespace, curlPodName, podIP, 9187)
				Expect(err).ToNot(HaveOccurred())
				expectedMetrics, err := buildMetrics(metricsMap, namespace, metricsClusterName, pod)
				Expect(err).ToNot(HaveOccurred())
				AssertMetrics(out, expectedMetrics, pod.GetName())
			}
		})

		// verify cnpg_collector_x metrics is exists in each pod
		collectAndAssertCollectorMetricsPresentOnEachPod(namespace, metricsClusterName,
			curlPodName)
	})

	It("can gather metrics with multiple target databases", func() {
		const namespacePrefix = "metrics-target-databases-e2e"
		metricsClusterName, err = env.GetResourceNameFromYAML(clusterMetricsDBFile)
		Expect(err).ToNot(HaveOccurred())
		// Create the cluster namespace
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		AssertCustomMetricsResourcesExist(namespace, customQueriesSampleFile, 1, 1)

		// Create the curl client pod and wait for it to be ready.
		By("setting up curl client pod", func() {
			curlClient := utils.CurlClient(namespace)
			err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
			Expect(err).ToNot(HaveOccurred())
			curlPodName = curlClient.GetName()
		})

		// Create the cluster
		AssertCreateCluster(namespace, metricsClusterName, clusterMetricsDBFile, env)
		AssertCreationOfTestDataForTargetDB(namespace, metricsClusterName, targetDBOne, testTableName, psqlClientPod)
		AssertCreationOfTestDataForTargetDB(namespace, metricsClusterName, targetDBTwo, testTableName, psqlClientPod)
		AssertCreationOfTestDataForTargetDB(namespace, metricsClusterName, targetDBSecret, testTableName, psqlClientPod)

		cluster, err := env.GetCluster(namespace, metricsClusterName)
		Expect(err).ToNot(HaveOccurred())

		AssertMetricsData(namespace, curlPodName, targetDBOne, targetDBTwo, targetDBSecret, cluster)
	})

	It("can gather default metrics details", func() {
		const clusterWithDefaultMetricsFile = fixturesDir + "/base/cluster-storage-class.yaml.template"
		const namespacePrefix = "default-metrics-details"
		metricsClusterName, err = env.GetResourceNameFromYAML(clusterWithDefaultMetricsFile)
		Expect(err).ToNot(HaveOccurred())
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		// Create the curl client pod and wait for it to be ready.
		By("setting up curl client pod", func() {
			curlClient := utils.CurlClient(namespace)
			err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
			Expect(err).ToNot(HaveOccurred())
			curlPodName = curlClient.GetName()
		})

		AssertCreateCluster(namespace, metricsClusterName, clusterWithDefaultMetricsFile, env)

		By("verify default monitoring configMap in cluster namespace", func() {
			// verify that, configMap should be created in cluster
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				err = env.Client.Get(
					env.Ctx,
					ctrlclient.ObjectKey{Namespace: namespace, Name: defaultMonitoringConfigMapName},
					configMap)
				return err
			}, 10).ShouldNot(HaveOccurred())
		})

		collectAndAssertDefaultMetricsPresentOnEachPod(namespace, metricsClusterName, curlPodName, true)
	})

	It("default set of metrics queries should not be injected into the cluster "+
		"when disableDefaultQueries field set to be true", func() {
		const defaultMonitoringQueriesDisableSampleFile = fixturesDir +
			"/metrics/cluster-disable-default-metrics.yaml.template"
		const namespacePrefix = "disable-default-metrics"
		metricsClusterName, err = env.GetResourceNameFromYAML(defaultMonitoringQueriesDisableSampleFile)
		Expect(err).ToNot(HaveOccurred())
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		// Create the curl client pod and wait for it to be ready.
		By("setting up curl client pod", func() {
			curlClient := utils.CurlClient(namespace)
			err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
			Expect(err).ToNot(HaveOccurred())
			curlPodName = curlClient.GetName()
		})

		// Create the cluster
		AssertCreateCluster(namespace, metricsClusterName, defaultMonitoringQueriesDisableSampleFile, env)

		collectAndAssertDefaultMetricsPresentOnEachPod(namespace, metricsClusterName, curlPodName, false)
	})

	It("execute custom queries against the application database on replica clusters", func() {
		const (
			replicaModeClusterDir    = "/replica_mode_cluster/"
			replicaClusterSampleFile = fixturesDir + "/metrics/cluster-replica-tls-with-metrics.yaml.template"
			srcClusterSampleFile     = fixturesDir + replicaModeClusterDir + "cluster-replica-src.yaml.template"
			configMapFIle            = fixturesDir + "/metrics/custom-queries-for-replica-cluster.yaml"
			checkQuery               = "SELECT count(*) FROM test_replica"
		)

		const namespacePrefix = "metrics-with-replica-mode"

		// Fetching the source cluster name
		srcClusterName, err := env.GetResourceNameFromYAML(srcClusterSampleFile)
		Expect(err).ToNot(HaveOccurred())

		// Fetching replica cluster name
		replicaClusterName, err := env.GetResourceNameFromYAML(replicaClusterSampleFile)
		Expect(err).ToNot(HaveOccurred())

		// create namespace
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		// Creating and verifying custom queries configmap
		AssertCustomMetricsResourcesExist(namespace, configMapFIle, 1, 0)

		// Create the curl client pod and wait for it to be ready
		By("setting up curl client pod", func() {
			curlClient := utils.CurlClient(namespace)
			err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
			Expect(err).ToNot(HaveOccurred())
			curlPodName = curlClient.GetName()
		})

		// Create the source Cluster
		AssertCreateCluster(namespace, srcClusterName, srcClusterSampleFile, env)

		// Create the replica Cluster
		AssertReplicaModeCluster(
			namespace,
			srcClusterName,
			replicaClusterSampleFile,
			checkQuery,
			psqlClientPod)

		By("grant select permission for test_replica table to pg_monitor", func() {
			cmd := "GRANT SELECT ON test_replica TO pg_monitor"
			superUser, superUserPass, err := utils.GetCredentials(srcClusterName, namespace, apiv1.SuperUserSecretSuffix, env)
			Expect(err).ToNot(HaveOccurred())
			host, err := utils.GetHostName(namespace, srcClusterName, env)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = utils.RunQueryFromPod(
				psqlClientPod,
				host,
				"appSrc",
				superUser,
				superUserPass,
				cmd,
				env)
			Expect(err).ToNot(HaveOccurred())
		})

		By("collecting metrics on each pod and checking that the table has been found", func() {
			podList, err := env.GetClusterPodList(namespace, replicaClusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather metrics in each pod
			for _, pod := range podList.Items {
				podIP := pod.Status.PodIP
				out, err := utils.CurlGetMetrics(namespace, curlPodName, podIP, 9187)
				Expect(err).Should(Not(HaveOccurred()))
				Expect(strings.Split(out, "\n")).Should(ContainElement("cnpg_replica_test_row_count 3"))
			}
		})
		collectAndAssertDefaultMetricsPresentOnEachPod(namespace, replicaClusterName, curlPodName, true)
	})
})
