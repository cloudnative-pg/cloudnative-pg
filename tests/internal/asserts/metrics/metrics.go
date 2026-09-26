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

// Package metrics provides Ginkgo/Gomega assertions over the metrics
// scraped from instance pods: presence/absence checks and contents of
// custom metric ConfigMaps/Secrets.
package metrics

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/proxy"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertCustomMetricsResourcesExist creates the resources defined in the
// given sample file and verifies they produce the expected number of
// ConfigMaps and Secrets tagged with e2e=metrics.
func AssertCustomMetricsResourcesExist(
	env *environment.TestingEnvironment,
	namespace, sampleFile string,
	configMapsCount, secretsCount int,
) {
	GinkgoHelper()
	By("verifying the custom metrics ConfigMaps and Secrets exist", func() {
		resources.CreateResourceFromFile(env, namespace, sampleFile)

		timeout := 20
		Eventually(func() ([]corev1.ConfigMap, error) {
			cmList := &corev1.ConfigMapList{}
			err := env.Client.List(
				env.Ctx, cmList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"e2e": "metrics"},
			)
			return cmList.Items, err
		}, timeout).Should(HaveLen(configMapsCount))

		Eventually(func() ([]corev1.Secret, error) {
			secretList := &corev1.SecretList{}
			err := env.Client.List(
				env.Ctx, secretList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"e2e": "metrics"},
			)
			return secretList.Items, err
		}, timeout).Should(HaveLen(secretsCount))
	})
}

// AssertMetricsData scrapes each pod and verifies the target-database
// custom metrics are present. On the primary pod, it also expects the
// backup timestamp metrics to be exposed.
func AssertMetricsData(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, targetOne, targetTwo, targetSecret string,
	cluster *apiv1.Cluster,
) {
	GinkgoHelper()
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

// CollectAndAssertDefaultMetricsPresentOnEachPod verifies the default set
// of cnpg_* metrics is present (or absent) on every pod of the cluster.
func CollectAndAssertDefaultMetricsPresentOnEachPod(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName string,
	tlsEnabled bool,
	expectPresent bool,
) {
	GinkgoHelper()
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

				g.Expect(strings.Contains(out, "cnpg_collector_last_collection_error 0")).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
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

// CollectAndAssertCollectorMetricsPresentOnEachPod verifies the cnpg
// collector metrics (collection_duration, fencing, wal stats, …) are
// exposed on every pod of the cluster.
func CollectAndAssertCollectorMetricsPresentOnEachPod(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	cluster *apiv1.Cluster,
) {
	GinkgoHelper()
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

				g.Expect(strings.Contains(out, "cnpg_collector_last_collection_error 0")).Should(BeTrue(),
					"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
				for _, data := range cnpgCollectorMetrics {
					g.Expect(strings.Contains(out, data)).Should(BeTrue(),
						"Metric collection issues on pod %v."+
							"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
				}
			}, testTimeouts[timeouts.Short]).To(Succeed())
		}
	})
}

// AssertIncludesMetrics asserts that every expected metric name appears
// in rawMetricsOutput and that its value matches the supplied regexp.
func AssertIncludesMetrics(g Gomega, rawMetricsOutput string, expectedMetrics map[string]*regexp.Regexp) {
	debugDetails := fmt.Sprintf("Printing rawMetricsOutput:\n%s", rawMetricsOutput)
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

// AssertExcludesMetrics asserts that every entry in nonCollected is
// absent from rawMetricsOutput.
func AssertExcludesMetrics(g Gomega, rawMetricsOutput string, nonCollected []string) {
	for _, nonCollectable := range nonCollected {
		g.Expect(rawMetricsOutput).NotTo(ContainSubstring(nonCollectable))
	}
}
