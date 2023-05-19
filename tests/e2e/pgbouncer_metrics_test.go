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
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBouncer Metrics", Label(tests.LabelObservability), func() {
	const (
		cnpgCluster                 = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml.template"
		poolerBasicAuthRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		namespacePrefix             = "pgbouncer-metrics-e2e"
		level                       = tests.Low
	)
	var namespace string
	var clusterName, curlPodName string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("should retrieve the metrics exposed by a freshly created pooler of type pgBouncer and validate its content",
		func() {
			var err error
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})

			// Create the curl client pod and wait for it to be ready.
			By("setting up curl client pod", func() {
				curlClient := utils.CurlClient(namespace)
				err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
				Expect(err).ToNot(HaveOccurred())
				curlPodName = curlClient.GetName()
			})

			clusterName, err = env.GetResourceNameFromYAML(cnpgCluster)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, cnpgCluster, env)

			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)

			poolerName, err := env.GetResourceNameFromYAML(poolerBasicAuthRWSampleFile)
			Expect(err).ToNot(HaveOccurred())
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
			Expect(err).ToNot(HaveOccurred())

			promMetrics := []string{
				`cnpg_pgbouncer_collection_duration_seconds{collector="Collect.up"} [0-9e\.]+|`,
				`cnpg_pgbouncer_collections_total \d+|`,
				`cnpg_pgbouncer_last_collection_error 0|`,
				`cnpg_pgbouncer_lists_dns_pending \d+|`,
				`cnpg_pgbouncer_lists_dns_queries \d+|`,
				`cnpg_pgbouncer_lists_free_clients \d+|`,
				`cnpg_pgbouncer_lists_pools \d+|`,
				`cnpg_pgbouncer_lists_used_servers \d+|`,
				`cnpg_pgbouncer_lists_users \d+|`,
				`cnpg_pgbouncer_pools_cl_active{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_cl_waiting{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_cl_active_cancel_req{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_cl_waiting_cancel_req{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_pool_mode{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_active{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_active_cancel{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_idle{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_login{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_tested{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_used{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_pools_sv_wait_cancels{database="pgbouncer",user="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_stats_avg_query_time{database="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_stats_avg_recv{database="pgbouncer"} \d+|`,
				`cnpg_pgbouncer_stats_total_query_count{database="pgbouncer"} \d+`,
			}
			metricsRegexp := regexp.MustCompile(fmt.Sprintf("(?m:^(%s)$)",
				strings.Join(promMetrics, "")))

			for _, pod := range podList.Items {
				podName := pod.GetName()
				podIP := pod.Status.PodIP
				out, err := utils.CurlGetMetrics(namespace, curlPodName, podIP, 9127)
				Expect(err).ToNot(HaveOccurred())
				matches := metricsRegexp.FindAllString(out, -1)
				Expect(matches).To(
					HaveLen(len(promMetrics)),
					"Metric collection issues on %v.\nCollected metrics:\n%v",
					podName,
					out,
				)
			}
		})
})
