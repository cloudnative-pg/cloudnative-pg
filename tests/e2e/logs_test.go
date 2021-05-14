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
	const namespace = "logs-e2e"
	const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
	const clusterName = "postgresql-storage-class"

	AssertPodLogs := func(namespace string, podName string, testQuery string) {
		isPgControlDataLoggerFound := false
		isPostgresLoggerFound := false
		isErrorMsgFoundInLoggingCollector := false
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		podObj := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, podObj)
		Expect(err).ToNot(HaveOccurred())
		commandTimeout := time.Second * 5
		// Execute a wrong query and verify this error inside the instance logs
		_, _, err = env.ExecCommand(env.Ctx, *podObj, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", testQuery)
		Expect(err).To(HaveOccurred())
		expectedResult := err.Error()
		// Gather pod logs
		podLogs, err := env.GetPodLogs(namespace, podName)
		Expect(err).ToNot(HaveOccurred())
		// In pod logs, each row is stored as JSON object. Split the JSON objects into JSON array
		data := strings.Split(podLogs, "\n")
		Expect(len(data) > 1).Should(BeTrue(), fmt.Sprintf("No logs found for pod %v", podName))
		for index, item := range data {
			// Store unmarshal items for further process
			var unmarshalItem map[string]interface{}
			if item != "" {
				err := json.Unmarshal([]byte(item), &unmarshalItem)
				// If unsupported format log line will be present in pod logs, then it will expect this error
				Expect(err).ShouldNot(HaveOccurred(),
					fmt.Sprintf("Unexpected log format found '%v' in pod %v logs on line number %v",
						item, podName, index))
				if value, ok := unmarshalItem["logger"]; ok {
					switch {
					case value == "pg_controldata":
						isPgControlDataLoggerFound = true
						// Verify that PG_CONTROLDATA logger exists in pod logs
						// and the message value should not be empty
						Expect(unmarshalItem["msg"]).Should(Not(BeEmpty()))
					case value == "postgres":
						isPostgresLoggerFound = true
						// Verify that POSTGRES logger exists in pod logs
						// and the message value should not be empty
						Expect(unmarshalItem["msg"]).Should(Not(BeEmpty()))
						recordField, recordOk := unmarshalItem["record"]
						if recordOk {
							// Record will be a JSON object, so we need to map keys and values as structured format
							record := recordField.(map[string]interface{})
							if strings.Contains(expectedResult, record["message"].(string)) {
								isErrorMsgFoundInLoggingCollector = true
								getExecutedQuery := record["query"]
								Expect(getExecutedQuery).Should(Not(BeEmpty()),
									fmt.Sprintf("Query record for pod '%v' is empty", podName))
								Expect(getExecutedQuery).Should(BeEquivalentTo(testQuery))
								Expect(record["user_name"]).Should(BeEquivalentTo("postgres"))
								Expect(record["database_name"]).Should(BeEquivalentTo("app"))
							}
						}
					}
				}
			}
		}
		// Verify all the expected loggers ie.'PG_CONTROLDATA','POSTGRES' and 'LOGGING_COLLECTOR' will be
		// found into pod logs
		Expect(isPgControlDataLoggerFound).Should(BeTrue(),
			fmt.Sprintf("pg_controldata logger is not found in pod %v logs", podName))
		Expect(isPostgresLoggerFound).Should(BeTrue(),
			fmt.Sprintf("postgres logger is not found in pod %v logs", podName))
		Expect(isErrorMsgFoundInLoggingCollector).Should(BeTrue(),
			fmt.Sprintf("Error message in logging_collector logger is not found in pod %v log",
				podName))
	}
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
	It("can gather json logs from PostgreSQL instances", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("verifying each PostgreSQL instance logs correctly", func() {
			errorTestQuery := "selecct 1\nwith newlines\n"
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				AssertPodLogs(namespace, pod.GetName(), errorTestQuery)
			}
		})
		By("verifying only the primary instance logs write queries", func() {
			errorTestQuery := "ccreate table test(var text)"
			primaryPod, _ := env.GetClusterPrimary(namespace, clusterName)
			AssertPodLogs(namespace, primaryPod.GetName(), errorTestQuery)
			// Verify 'test query text' exists or not, into standby instances logs
			podList := &corev1.PodList{}
			err = env.Client.List(
				env.Ctx, podList, client.InNamespace(namespace),
				client.MatchingLabels{"postgresql": clusterName, "role": "replica"},
			)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				isQueryTextFoundInLoggingCollector := false
				podName := pod.GetName()
				podLogs, err := env.GetPodLogs(namespace, podName)
				Expect(err).ToNot(HaveOccurred())
				// In pod logs, each row is stored as JSON object. Split the JSON objects into JSON array
				data := strings.Split(podLogs, "\n")
				Expect(len(data) > 1).Should(BeTrue(), fmt.Sprintf("No logs found for pod %v", podName))
				for index, item := range data {
					// Store unmarshal items for further process
					var unmarshalItem map[string]interface{}
					if item != "" {
						err := json.Unmarshal([]byte(item), &unmarshalItem)
						// If unsupported format log line will be present in pod logs, then it will raise error
						Expect(err).ShouldNot(HaveOccurred(),
							fmt.Sprintf("Unexpected log format found '%v' in pod %v logs on line number %v",
								item, podName, index))
						if value, ok := unmarshalItem["logger"]; ok {
							if value == "postgres" {
								recordField, recordOk := unmarshalItem["record"]
								if recordOk {
									// Record will be a JSON object, so we need to map keys and values as structured format
									record := recordField.(map[string]interface{})
									getExecutedQuery := record["query"]
									if getExecutedQuery == errorTestQuery {
										// If the logging collector will be found inside a standby instance that logs
										// query text, then we will set flag true
										isQueryTextFoundInLoggingCollector = true
									}
								}
							}
						}
					}
				}
				Expect(isQueryTextFoundInLoggingCollector).Should(BeFalse(),
					fmt.Sprintf("Error logs of write queries have been also collected on replica %v", podName))
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
			// Verify pg_rewind logging exists or not in previous primary instance logs
			isPgRewindLoggerFound := false
			oldPrimaryPod := currentPrimary.GetName()
			podLogs, err := env.GetPodLogs(namespace, oldPrimaryPod)
			Expect(err).ToNot(HaveOccurred())
			// In pod logs, each row is stored as a JSON object. Split the JSON objects into JSON array
			data := strings.Split(podLogs, "\n")
			Expect(len(data) > 1).Should(BeTrue(), fmt.Sprintf("No logs found for pod %v", oldPrimaryPod))
			for index, item := range data {
				// Store unmarshal items for further process
				var unmarshalItem map[string]interface{}
				if item != "" {
					err = json.Unmarshal([]byte(item), &unmarshalItem)
					// If unsupported format log line will present in pod logs, then it will raise error
					Expect(err).ShouldNot(HaveOccurred(),
						fmt.Sprintf("Unexpected log format found '%v' in pod %v logs on line number %v",
							item, oldPrimaryPod, index))
					if value, ok := unmarshalItem["logger"]; ok {
						if value == "pg_rewind" {
							isPgRewindLoggerFound = true
						}
					}
				}
			}
			Expect(isPgRewindLoggerFound).Should(BeTrue(),
				fmt.Sprintf("pg_rewind logger hasn't been found in the oldprimary pod %v logs", oldPrimaryPod))
		})
	})
})
