/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"regexp"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBouncer Metrics", func() {
	const (
		cnpCluster                  = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerBasicAuthRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		namespace                   = "pgbouncer-metrics-e2e"
		level                       = tests.Low
	)
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

			clusterName, err = env.GetResourceNameFromYAML(cnpCluster)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, cnpCluster, env)

			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)

			podList, err := getPGBouncerPodList(namespace, poolerBasicAuthRWSampleFile)
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

			podCommandResults, err := tests.RunOnPodList(namespace, "sh -c 'curl -s 127.0.0.1:9127/metrics'", podList)
			Expect(err).ToNot(HaveOccurred())

			for _, podCommandResult := range podCommandResults {
				matches := metricsRegexp.FindAllString(podCommandResult.Output, -1)
				Expect(matches).To(
					HaveLen(14),
					"Metric collection issues on %v.\nCollected metrics:\n%v",
					podCommandResult.Pod.GetName(),
					podCommandResult.Output,
				)
			}
		})
})
