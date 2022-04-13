/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package e2e

import (
	"regexp"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBouncer Metrics", func() {
	const (
		cnpCluster                  = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerBasicAuthRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		namespace                   = "pgbouncer-metrics-e2e"
		level                       = tests.Low
	)

	var clusterName, curlPodName string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should retrieve the metrics exposed by a freshly created pooler of type pgBouncer and validate its content",
		func() {
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// Create the curl client pod and wait for it to be ready.
			By("setting up curl client pod", func() {
				curlClient := utils.CurlClient(namespace)
				err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
				Expect(err).ToNot(HaveOccurred())
				curlPodName = curlClient.GetName()
			})

			clusterName, err = env.GetResourceNameFromYAML(cnpCluster)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, cnpCluster, env)

			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)

			poolerName, err := env.GetResourceNameFromYAML(poolerBasicAuthRWSampleFile)
			Expect(err).ToNot(HaveOccurred())
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
			Expect(err).ToNot(HaveOccurred())

			metricsRegexp := regexp.MustCompile(
				`(?m:^(` +
					`cnp_pgbouncer_collection_duration_seconds{collector="Collect.up"} [0-9e\+\.]+|` +
					`cnp_pgbouncer_collections_total 1|` +
					`cnp_pgbouncer_last_collection_error 0|` +
					`cnp_pgbouncer_lists_dns_pending 0|` +
					`cnp_pgbouncer_lists_dns_queries 0|` +
					`cnp_pgbouncer_lists_free_clients 49|` +
					`cnp_pgbouncer_lists_pools 1|` +
					`cnp_pgbouncer_lists_used_servers 0|` +
					`cnp_pgbouncer_lists_users 2|` +
					`cnp_pgbouncer_pools_cl_cancel_req{database="pgbouncer",user="pgbouncer"} 0|` +
					`cnp_pgbouncer_pools_pool_mode{database="pgbouncer",user="pgbouncer"} 3|` +
					`cnp_pgbouncer_stats_avg_query_time{database="pgbouncer"} [0-9e\+\.]+|` +
					`cnp_pgbouncer_stats_avg_recv{database="pgbouncer"} [0-9e\+\.]+|` +
					`cnp_pgbouncer_stats_total_query_count{database="pgbouncer"} \d+` +
					`)$)`)

			for _, pod := range podList.Items {
				podName := pod.GetName()
				podIP := pod.Status.PodIP
				out, err := utils.CurlGetMetrics(namespace, curlPodName, podIP, 9127)
				Expect(err).ToNot(HaveOccurred())
				matches := metricsRegexp.FindAllString(out, -1)
				Expect(matches).To(
					HaveLen(14),
					"Metric collection issues on %v.\nCollected metrics:\n%v",
					podName,
					out,
				)
			}
		})
})
