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
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSON log output", func() {
	const namespace = "json-logs-e2e"
	const sampleFile = fixturesDir + "/json_logs/cluster-json-logs.yaml"
	const clusterName = "postgresql-json-logs"
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

	// parseJSONLogs returns the pod's logs of a given pod name,
	// in the form of a list of JSON entries
	parseJSONLogs := func(namespace string, podName string) []string {
		// Gather pod logs
		podLogs, err := env.GetPodLogs(namespace, podName)
		Expect(err).ToNot(HaveOccurred())

		// In pod logs, each row is stored as JSON object. Split the JSON objects into JSON array
		logEntries := strings.Split(podLogs, "\n")
		Expect(len(logEntries) > 1).Should(BeTrue(), fmt.Sprintf("No logs found for pod %v", podName))

		return logEntries
	}

	// assertLoggerField verifies if a given value exists inside a list of JSON entries
	assertLoggerField := func(logEntries []string, podName string, value string) bool {
		var unmarshalItem map[string]interface{}
		for i, logEntry := range logEntries {
			if logEntry != "" {
				err := json.Unmarshal([]byte(logEntry), &unmarshalItem)
				Expect(err).ShouldNot(HaveOccurred(),
					fmt.Sprintf("Unexpected log format found '%v' in pod %v logs on line number %v",
						logEntry, podName, i))
				if unmarshalItem["logger"] == value {
					Expect(unmarshalItem["msg"]).To(Not(BeEmpty()))
					return true
				}
			}
		}
		return false
	}

	// ensureLogIsWellFormed verifies if the message record field of the given map
	// contains the expectedResult string. If the message field matches the expectedResult,
	// then the related record is returned. Otherwise return nil.
	ensureLogIsWellFormed := func(item map[string]interface{}, expectedResult string) map[string]interface{} {
		if item["logger"] == "postgres" {
			Expect(item["msg"]).To(Not(BeEmpty()))
			// Filter out items with an empty record field
			if recordField, recordOk := item["record"]; recordOk {
				// The record will be a JSON object, so we need to map keys and values as a structured format
				record := recordField.(map[string]interface{})
				Expect(record["message"]).NotTo(BeNil())
				if strings.Contains(expectedResult, record["message"].(string)) {
					return record
				}
			}
		}
		return nil
	}

	// assertRecord applies some assertions related to the format of the JSON log fields keys and values
	assertRecord := func(record map[string]interface{}, errorTestQuery string) bool {
		// JSON entry assertions
		Expect(record["user_name"]).To(BeEquivalentTo("postgres"))
		Expect(record["database_name"]).To(BeEquivalentTo("app"))
		Expect(record["query"]).To(BeEquivalentTo(errorTestQuery))

		// Check the format of the log_time field
		timeFormat := "2006-01-02 15:04:05.999 UTC"
		_, err := time.Parse(timeFormat, record["log_time"].(string))
		Expect(err).ToNot(HaveOccurred())

		return true
	}

	// assertQueryRecord verifies if any of the message record field of each JSON row
	// contains the expectedResult string, then applies the assertions related to the log format
	assertQueryRecord := func(logEntries []string, errorTestQuery string, expectedResult string) bool {
		for _, logEntry := range logEntries {
			var unmarshalItem map[string]interface{}
			if logEntry != "" {
				err := json.Unmarshal([]byte(logEntry), &unmarshalItem)
				Expect(err).ToNot(HaveOccurred())
				record := ensureLogIsWellFormed(unmarshalItem, expectedResult)
				if record != nil {
					return assertRecord(record, errorTestQuery)
				}
			}
		}
		return false
	}

	It("correctly produces logs in JSON format", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("verifying the presence of possible logger values", func() {
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				// Gather pod logs in the form of a Json Array
				logEntries := parseJSONLogs(namespace, pod.GetName())

				// Logger field Assertions
				isPgControlDataLoggerFound := assertLoggerField(logEntries, pod.GetName(), "pg_controldata")
				Expect(isPgControlDataLoggerFound).To(BeTrue(),
					fmt.Sprintf("pg_controldata logger not found in pod %v logs", pod.GetName()))
				isPostgresLoggerFound := assertLoggerField(logEntries, pod.GetName(), "postgres")
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
				Eventually(func() bool {
					// Gather pod logs in the form of a Json Array
					logEntries := parseJSONLogs(namespace, pod.GetName())

					// Gather the record containing the wrong query result
					return assertQueryRecord(logEntries, errorTestQuery, expectedResult)
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

			// Expect the query to eventually be logged on the primary
			Eventually(func() bool {
				// Gather pod logs in the form of a Json Array
				logEntries := parseJSONLogs(namespace, primaryPod.GetName())

				// Gather the record containing the wrong query result
				return assertQueryRecord(logEntries, errorTestQuery, expectedResult)
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
				logEntries := parseJSONLogs(namespace, pod.GetName())

				// No record should be returned in this case
				Expect(assertQueryRecord(logEntries, expectedResult, errorTestQuery)).Should(BeFalse())
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

			Eventually(func() bool {
				// Gather pod logs in the form of a JSON slice
				logEntries := parseJSONLogs(namespace, currentPrimary.GetName())

				// Expect pg_rewind logger to eventually be present on the old primary logs
				isPgRewindLoggerFound := assertLoggerField(logEntries, currentPrimary.GetName(), "pg_rewind")
				return isPgRewindLoggerFound
			}, timeout).Should(BeTrue())
		})
	})
})
