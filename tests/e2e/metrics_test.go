/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", func() {
	// Cluster identifiers
	const namespace = "cluster-metrics-e2e"
	const clusterMetricsFile = fixturesDir + "/metrics/cluster-metrics.yaml"
	const clusterName = "postgresql-metrics"
	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})
	It("can gather metrics", func() {
		// Create the cluster namespace
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		By("verifying the custom metrics ConfigMaps and Secrets exist", func() {
			// Create the ConfigMaps and a Secret
			customQueries := fixturesDir + "/metrics/custom-queries.yaml"
			_, _, err = tests.Run("kubectl apply -n " + namespace + " -f " + customQueries)
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
			}, timeout).Should(HaveLen(2))

			// Check secret exists
			Eventually(func() ([]corev1.Secret, error) {
				secretList := &corev1.SecretList{}
				err := env.Client.List(
					env.Ctx, secretList, client.InNamespace(namespace),
					client.MatchingLabels{"e2e": "metrics"},
				)
				return secretList.Items, err
			}, timeout).Should(HaveLen(1))
		})

		// Create the cluster
		AssertCreateCluster(namespace, clusterName, clusterMetricsFile, env)

		By("collecting metrics on each pod", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			// Gather metrics in each pod
			for _, pod := range podList.Items {
				pod := pod // pin the variable
				// We define a few metrics in the tests. We check that all of them exist and
				// there are no errors during the collection.
				re := regexp.MustCompile(
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
				metricsCmd := "sh -c 'curl -s 127.0.0.1:9187/metrics'"
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					pod.GetName(),
					metricsCmd))
				matches := re.FindAllString(out, -1)
				Expect(matches, err).To(HaveLen(8), "Metric collection issues on %v.\nCollected metrics:\n%v", pod.Name, out)
			}
		})
	})
})
