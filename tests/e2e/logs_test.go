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
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSON log output", Label(tests.LabelObservability), func() {
	var namespace, clusterName string
	const level = tests.Low

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("correctly produces logs in JSON format", func() {
		const namespacePrefix = "json-logs-e2e"
		clusterName = "postgresql-json-logs"
		const sampleFile = fixturesDir + "/json_logs/cluster-json-logs.yaml.template"
		var namespaceErr error
		// Create a cluster in a namespace we'll delete after the test
		namespace, namespaceErr = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(namespaceErr).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("verifying the presence of possible logger values", func() {
			podList, _ := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			for _, pod := range podList.Items {
				// Gather pod logs in the form of a Json Array
				logEntries, err := logs.ParseJSONLogs(
					env.Ctx, env.Interface,
					namespace, pod.GetName(),
				)
				Expect(err).NotTo(HaveOccurred(), "unable to parse json logs")
				Expect(logEntries).ToNot(BeEmpty(), "no logs found")

				// Logger field Assertions
				isPgControlDataLoggerFound := logs.HasLogger(logEntries, "pg_controldata")
				Expect(isPgControlDataLoggerFound).To(BeTrue(),
					fmt.Sprintf("pg_controldata logger not found in pod %v logs", pod.GetName()))
				isPostgresLoggerFound := logs.HasLogger(logEntries, "postgres")
				Expect(isPostgresLoggerFound).To(BeTrue(),
					fmt.Sprintf("postgres logger not found in pod %v logs", pod.GetName()))
			}
		})

		By("verifying the format of error queries being logged", func() {
			errorTestQuery := "selecct 1\nwith newlines\n"
			podList, _ := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			timeout := 300

			for _, pod := range podList.Items {
				var queryError error
				// Run a wrong query and save its result
				commandTimeout := time.Second * 10
				Eventually(func(_ Gomega) error {
					_, _, queryError = utils.ExecCommand(env.Ctx, env.Interface, env.RestClientConfig, pod,
						specs.PostgresContainerName, &commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
						errorTestQuery)
					return queryError
				}, RetryTimeout, PollingTime).ShouldNot(Succeed())

				// Eventually the error log line will be logged
				Eventually(func(g Gomega) bool {
					// Gather pod logs in the form of a Json Array
					logEntries, err := logs.ParseJSONLogs(
						env.Ctx, env.Interface,
						namespace, pod.GetName(),
					)
					g.Expect(err).ToNot(HaveOccurred())

					// Gather the record containing the wrong query result
					return logs.AssertQueryRecord(
						logEntries,
						errorTestQuery,
						queryError.Error(),
						logpipe.LoggingCollectorRecordName,
					)
				}, timeout).Should(BeTrue())
			}
		})

		By("verifying only the primary instance logs write queries", func() {
			errorTestQuery := "ccreate table test(var text)"
			primaryPod, _ := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			timeout := 300

			var queryError error
			// Run a wrong query on just the primary and save its result
			commandTimeout := time.Second * 10
			Eventually(func() error {
				_, _, queryError = utils.ExecCommand(env.Ctx, env.Interface, env.RestClientConfig,
					*primaryPod, specs.PostgresContainerName,
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", errorTestQuery)
				return queryError
			}, RetryTimeout, PollingTime).ShouldNot(Succeed())

			// Expect the query to be eventually logged on the primary
			Eventually(func() (bool, error) {
				// Gather pod logs in the form of a Json Array
				logEntries, err := logs.ParseJSONLogs(
					env.Ctx, env.Interface,
					namespace, primaryPod.GetName(),
				)
				if err != nil {
					GinkgoWriter.Printf("Error reported while gathering primary pod log %s\n", err.Error())
					return false, err
				}

				// Gather the record containing the wrong query result
				return logs.AssertQueryRecord(logEntries, errorTestQuery, queryError.Error(),
					logpipe.LoggingCollectorRecordName), nil
			}, timeout).Should(BeTrue())

			// Retrieve cluster replicas
			// Deprecated: Use utils.ClusterInstanceRoleLabelName instead of "role"
			podList := &corev1.PodList{}
			listError := env.Client.List(
				env.Ctx, podList, client.InNamespace(namespace),
				client.MatchingLabels{utils.ClusterLabelName: clusterName, "role": "replica"},
			)
			Expect(listError).ToNot(HaveOccurred())

			// Expect the query not to be logged on replicas
			for _, pod := range podList.Items {
				// Gather pod logs in the form of a Json Array
				logEntries, err := logs.ParseJSONLogs(
					env.Ctx, env.Interface,
					namespace, pod.GetName(),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(logEntries).ToNot(BeEmpty())

				// No record should be returned in this case
				isQueryRecordContained := logs.AssertQueryRecord(
					logEntries,
					queryError.Error(),
					errorTestQuery,
					logpipe.LoggingCollectorRecordName,
				)

				Expect(isQueryRecordContained).Should(BeFalse())
			}
		})

		By("verifying pg_rewind logs after deleting the old primary pod", func() {
			// Force-delete the primary
			currentPrimary, _ := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			quickDelete := &client.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}

			deletePodError := pods.Delete(env.Ctx, env.Client, namespace, currentPrimary.GetName(), quickDelete)
			Expect(deletePodError).ToNot(HaveOccurred())

			// Expect a new primary to be elected
			timeout := 180
			Eventually(func() (string, error) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					GinkgoWriter.Printf("Error reported while getting current primary %s\n", err.Error())
					return "", err
				}
				return cluster.Status.CurrentPrimary, err
			}, timeout).ShouldNot(BeEquivalentTo(currentPrimary))

			// Here we need to verify the number of the ready pods as well as wait for
			// the cluster status to be PhaseHealthy, using the AssertClusterIsReady.
			AssertClusterIsReady(namespace, clusterName, timeout, env)

			Eventually(func() (bool, error) {
				// Gather pod logs in the form of a JSON slice
				logEntries, err := logs.ParseJSONLogs(
					env.Ctx, env.Interface,
					namespace, currentPrimary.GetName(),
				)
				if err != nil {
					GinkgoWriter.Printf("Error reported while getting the 'pg_rewind' logger in old primary %s, %s\n",
						currentPrimary, err.Error())
					return false, err
				}
				// Expect pg_rewind logger to eventually be present on the old primary logs
				return logs.HasLogger(logEntries, "pg_rewind"), nil
			}, timeout).Should(BeTrue())
		})
	})
})

var _ = Describe("JSON log output unit tests", Label(tests.LabelObservability), func() {
	const errorTestQuery = "selecct 1\nwith newlines\n"
	const user = "postgres"
	const database = "app"
	const message = "syntax error at or near \"selecct\"etc etc"
	const record = "{\"level\":\"info\",\"ts\":1624458709.0748887,\"logger\":\"postgres\",\"msg\":\"record\"," +
		"\"record\":{\"log_time\":\"2021-06-23 14:31:49.074 UTC\",\"user_name\":\"postgres\",\"database_name\":\"app\"," +
		"\"process_id\":\"259\",\"connection_from\":\"[local]\",\"session_id\":\"60d345d5.103\"," +
		"\"session_line_num\":\"1\",\"command_tag\":\"idle\",\"session_start_time\":\"2021-06-23 14:31:49 UTC\"," +
		"\"virtual_transaction_id\":\"5/47\",\"transaction_id\":\"0\",\"error_severity\":\"ERROR\"," +
		"\"sql_state_code\":\"42601\",\"message\":\"syntax error at or near \\\"selecct\\\"\"," +
		"\"detail\":\"\",\"hint\":\"\",\"internal_query\":\"\",\"internal_query_pos\":\"\",\"context\":\"\"," +
		"\"query\":\"selecct 1\\nwith newlines\\n\",\"query_pos\":\"1\",\"location\":\"\",\"application_name\":\"psql\"," +
		"\"backend_type\":\"client backend\"}}"
	var parsedRecord map[string]any
	err := json.Unmarshal([]byte(record), &parsedRecord)
	Expect(err).ToNot(HaveOccurred())
	It("Can check valid logging_collector record for query", func() {
		Expect(parsedRecord).NotTo(BeNil())
		Expect(logs.CheckRecordForQuery(parsedRecord, errorTestQuery, user, database, message)).To(BeTrue())
	})
	It("Can check valid logging_collector ", func() {
		Expect(parsedRecord).NotTo(BeNil())
		Expect(logs.IsWellFormedLogForLogger(parsedRecord, "postgres")).To(BeTrue())
	})
})
