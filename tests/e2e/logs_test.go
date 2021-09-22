/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSON log output", func() {
	var namespace, clusterName string

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
				logEntries, err := parseJSONLogs(namespace, pod.GetName())
				Expect(err).NotTo(HaveOccurred(), "unable to parse json logs")
				Expect(len(logEntries) > 0).To(BeTrue(), "no logs found")

				// Logger field Assertions
				isPgControlDataLoggerFound := hasLogger(logEntries, "pg_controldata")
				Expect(isPgControlDataLoggerFound).To(BeTrue(),
					fmt.Sprintf("pg_controldata logger not found in pod %v logs", pod.GetName()))
				isPostgresLoggerFound := hasLogger(logEntries, "postgres")
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
					logEntries, err := parseJSONLogs(namespace, pod.GetName())
					if err != nil {
						return false, err
					}

					// Gather the record containing the wrong query result
					return assertQueryRecord(logEntries, errorTestQuery, expectedResult,
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
				logEntries, err := parseJSONLogs(namespace, primaryPod.GetName())
				if err != nil {
					return false, err
				}

				// Gather the record containing the wrong query result
				return assertQueryRecord(logEntries, errorTestQuery, expectedResult,
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
				logEntries, err := parseJSONLogs(namespace, pod.GetName())
				Expect(err).NotTo(HaveOccurred())
				Expect(len(logEntries) > 0).To(BeTrue())

				// No record should be returned in this case
				Expect(assertQueryRecord(logEntries, expectedResult, errorTestQuery,
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
				logEntries, err := parseJSONLogs(namespace, currentPrimary.GetName())
				if err != nil {
					return false, err
				}
				// Expect pg_rewind logger to eventually be present on the old primary logs
				return hasLogger(logEntries, "pg_rewind"), nil
			}, timeout).Should(BeTrue())
		})
	})
})

// parseJSONLogs returns the pod's logs of a given pod name,
// in the form of a list of JSON entries
func parseJSONLogs(namespace string, podName string) ([]map[string]interface{}, error) {
	// Gather pod logs
	podLogs, err := env.GetPodLogs(namespace, podName)
	if err != nil {
		return nil, err
	}

	// In pod logs, each row is stored as JSON object. Split the JSON objects into JSON array
	logEntries := strings.Split(podLogs, "\n")
	parsedLogEntries := make([]map[string]interface{}, len(logEntries))
	for i, entry := range logEntries {
		if entry == "" {
			continue
		}
		parsedEntry := make(map[string]interface{})
		err := json.Unmarshal([]byte(entry), &parsedEntry)
		if err != nil {
			return nil, err
		}
		parsedLogEntries[i] = parsedEntry
	}
	return parsedLogEntries, nil
}

// hasLogger verifies if a given value exists inside a list of JSON entries
func hasLogger(logEntries []map[string]interface{}, logger string) bool {
	for _, logEntry := range logEntries {
		if logEntry["logger"] == logger {
			return true
		}
	}
	return false
}

// assertQueryRecord verifies if any of the message record field of each JSON row
// contains the expectedResult string, then applies the assertions related to the log format
func assertQueryRecord(logEntries []map[string]interface{}, errorTestQuery string, message string, logger string) bool {
	for _, logEntry := range logEntries {
		if isWellFormedLogForLogger(logEntry, logger) &&
			checkRecordForQuery(logEntry, errorTestQuery, "postgres", "app", message) {
			return true
		}
	}
	return false
}

// isWellFormedLogForLogger verifies if the message record field of the given map
// contains the expectedResult string. If the message field matches the expectedResult,
// then the related record is returned. Otherwise return nil.
func isWellFormedLogForLogger(item map[string]interface{}, loggerField string) bool {
	if logger, ok := item["logger"]; !ok || logger != loggerField {
		return false
	}
	if msg, ok := item["msg"]; !ok || msg == "" {
		return false
	}
	if record, ok := item["record"]; ok && record != "" {
		_, ok := record.(map[string]interface{})
		if !ok {
			return false
		}
	}

	return true
}

// checkRecordForQuery applies some assertions related to the format of the JSON log fields keys and values
func checkRecordForQuery(entry map[string]interface{}, errorTestQuery, user, database, message string) bool {
	record, ok := entry["record"]
	if !ok || record == nil {
		return false
	}
	recordMap, isMap := record.(map[string]interface{})
	// JSON entry assertions
	if !isMap || recordMap["user_name"] != user ||
		recordMap["database_name"] != database ||
		recordMap["query"] != errorTestQuery ||
		!strings.Contains(message, recordMap["message"].(string)) {
		return false
	}

	// Check the format of the log_time field
	timeFormat := "2006-01-02 15:04:05.999 UTC"
	_, err := time.Parse(timeFormat, recordMap["log_time"].(string))
	return err == nil
}

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
		Expect(checkRecordForQuery(parsedRecord, errorTestQuery, user, database, message)).To(BeTrue())
	})
	It("Can check valid logging_collector ", func() {
		Expect(parsedRecord).NotTo(BeNil())
		Expect(isWellFormedLogForLogger(parsedRecord, "postgres")).To(BeTrue())
	})
})
