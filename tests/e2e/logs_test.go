/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	testsUtils "github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSON log output", func() {
	var namespace, clusterName string
	const level = tests.Low

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

	It("correctly produces logs in JSON format", func() {
		namespace = "json-logs-e2e"
		clusterName = "postgresql-json-logs"
		const sampleFile = fixturesDir + "/json_logs/cluster-json-logs.yaml"
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("verifying the presence of possible logger values", func() {
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				// Gather pod logs in the form of a Json Array
				logEntries, err := testsUtils.ParseJSONLogs(namespace, pod.GetName(), env)
				Expect(err).NotTo(HaveOccurred(), "unable to parse json logs")
				Expect(len(logEntries) > 0).To(BeTrue(), "no logs found")

				// Logger field Assertions
				isPgControlDataLoggerFound := testsUtils.HasLogger(logEntries, "pg_controldata")
				Expect(isPgControlDataLoggerFound).To(BeTrue(),
					fmt.Sprintf("pg_controldata logger not found in pod %v logs", pod.GetName()))
				isPostgresLoggerFound := testsUtils.HasLogger(logEntries, "postgres")
				Expect(isPostgresLoggerFound).To(BeTrue(),
					fmt.Sprintf("postgres logger not found in pod %v logs", pod.GetName()))
			}
		})

		By("verifying the format of error queries being logged", func() {
			errorTestQuery := "selecct 1\nwith newlines\n"
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			timeout := 300

			for _, pod := range podList.Items {
				// Run a wrong query and save its result
				commandTimeout := time.Second * 5
				_, _, err = env.ExecCommand(env.Ctx, pod, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", errorTestQuery)
				Expect(err).To(HaveOccurred())
				expectedResult := err.Error()

				// Eventually the error log line will be logged
				Eventually(func() (bool, error) {
					// Gather pod logs in the form of a Json Array
					logEntries, err := testsUtils.ParseJSONLogs(namespace, pod.GetName(), env)
					if err != nil {
						return false, err
					}

					// Gather the record containing the wrong query result
					return testsUtils.AssertQueryRecord(logEntries, errorTestQuery, expectedResult,
						logpipe.LoggingCollectorRecordName), nil
				}, timeout).Should(BeTrue())
			}
		})

		By("verifying only the primary instance logs write queries", func() {
			errorTestQuery := "ccreate table test(var text)"
			primaryPod, _ := env.GetClusterPrimary(namespace, clusterName)
			timeout := 300

			// Run a wrong query on just the primary and save its result
			commandTimeout := time.Second * 5
			_, _, err = env.ExecCommand(env.Ctx, *primaryPod, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", errorTestQuery)
			Expect(err).To(HaveOccurred())
			expectedResult := err.Error()

			// Expect the query to be eventually logged on the primary
			Eventually(func() (bool, error) {
				// Gather pod logs in the form of a Json Array
				logEntries, err := testsUtils.ParseJSONLogs(namespace, primaryPod.GetName(), env)
				if err != nil {
					return false, err
				}

				// Gather the record containing the wrong query result
				return testsUtils.AssertQueryRecord(logEntries, errorTestQuery, expectedResult,
					logpipe.LoggingCollectorRecordName), nil
			}, timeout).Should(BeTrue())

			// Retrieve cluster replicas
			podList := &corev1.PodList{}
			err = env.Client.List(
				env.Ctx, podList, client.InNamespace(namespace),
				client.MatchingLabels{"postgresql": clusterName, "role": "replica"},
			)
			Expect(err).ToNot(HaveOccurred())

			// Expect the query not to be logged on replicas
			for _, pod := range podList.Items {
				// Gather pod logs in the form of a Json Array
				logEntries, err := testsUtils.ParseJSONLogs(namespace, pod.GetName(), env)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(logEntries) > 0).To(BeTrue())

				// No record should be returned in this case
				Expect(testsUtils.AssertQueryRecord(logEntries, expectedResult, errorTestQuery,
					logpipe.LoggingCollectorRecordName)).Should(BeFalse())
			}
		})

		By("verifying pg_rewind logs after deleting the old primary pod", func() {
			// Force-delete the primary
			zero := int64(0)
			currentPrimary, _ := env.GetClusterPrimary(namespace, clusterName)
			forceDelete := &client.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			err = env.DeletePod(namespace, currentPrimary.GetName(), forceDelete)
			Expect(err).ToNot(HaveOccurred())

			// Expect a new primary to be elected
			timeout := 180
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			Eventually(func() (string, error) {
				cluster := &apiv1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.CurrentPrimary, err
			}, timeout).ShouldNot(BeEquivalentTo(currentPrimary))

			// Wait for the pods to be ready again
			Eventually(func() (int, error) {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				return utils.CountReadyPods(podList.Items), err
			}, timeout).Should(BeEquivalentTo(3))

			Eventually(func() (bool, error) {
				// Gather pod logs in the form of a JSON slice
				logEntries, err := testsUtils.ParseJSONLogs(namespace, currentPrimary.GetName(), env)
				if err != nil {
					return false, err
				}
				// Expect pg_rewind logger to eventually be present on the old primary logs
				return testsUtils.HasLogger(logEntries, "pg_rewind"), nil
			}, timeout).Should(BeTrue())
		})
	})
})

var _ = Describe("JSON log output unit tests", func() {
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
	var parsedRecord map[string]interface{}
	err := json.Unmarshal([]byte(record), &parsedRecord)
	Expect(err).To(BeNil())
	It("Can check valid logging_collector record for query", func() {
		Expect(parsedRecord).NotTo(BeNil())
		Expect(testsUtils.CheckRecordForQuery(parsedRecord, errorTestQuery, user, database, message)).To(BeTrue())
	})
	It("Can check valid logging_collector ", func() {
		Expect(parsedRecord).NotTo(BeNil())
		Expect(testsUtils.IsWellFormedLogForLogger(parsedRecord, "postgres")).To(BeTrue())
	})
})
