/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// AssertCreateCluster tests that the pods that should have been created by the sample
// exist and are in ready state
func AssertCreateCluster(namespace string, clusterName string, sample string, env *tests.TestingEnvironment) {
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		timeout := 20
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}

		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, timeout).Should(BeEquivalentTo(namespace))
	})
	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sample)
		Expect(err).ToNot(HaveOccurred())
	})
	// Setting up a cluster with three pods is slow, usually 200-600s
	AssertClusterIsReady(namespace, clusterName, 600, env)
}

func AssertClusterIsReady(namespace string, clusterName string, timeout int, env *tests.TestingEnvironment) {
	By("having a Cluster with each instance in status ready", func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster
		cluster := &apiv1.Cluster{}
		err := env.Client.Get(env.Ctx, namespacedName, cluster)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() (int, error) {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster := &apiv1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			readyInstances := utils.CountReadyPods(podList.Items)
			return readyInstances, err
		}, timeout).Should(BeEquivalentTo(cluster.Spec.Instances))
	})
}

// AssertConnection is used if a connection from a pod to a postgresql
// database works
func AssertConnection(host string, user string, dbname string,
	password string, queryingPod corev1.Pod, timeout int, env *tests.TestingEnvironment) {
	By(fmt.Sprintf("connecting to the %v service as %v", host, user), func() {
		Eventually(func() string {
			dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require", host, user, dbname, password)
			timeout := time.Second * 2
			stdout, _, err := env.ExecCommand(env.Ctx, queryingPod, "postgres", &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			if err != nil {
				return ""
			}
			return stdout
		}, timeout).Should(Equal("1\n"))
	})
}

// AssertOperatorPodUnchanged verifies that the pod has an expected name and never restarted
func AssertOperatorPodUnchanged(expectedOperatorPodName string) {
	operatorPod, err := env.GetOperatorPod()
	Expect(err).NotTo(HaveOccurred())
	Expect(operatorPod.GetName()).Should(BeEquivalentTo(expectedOperatorPodName),
		"Operator pod was recreated before the end of the test")
	restartCount := -1
	for _, containerStatus := range operatorPod.Status.ContainerStatuses {
		if containerStatus.Name == "manager" {
			restartCount = int(containerStatus.RestartCount)
		}
	}
	Expect(restartCount).Should(BeEquivalentTo(0), fmt.Sprintf("Operator pod get restarted %v times ", restartCount))
}

// AssertOperatorIsReady verifies that the operator is ready
func AssertOperatorIsReady() {
	Eventually(env.IsOperatorReady, 120).Should(BeTrue(), "Operator pod is not ready")
}

// AssertCreateTestData create test data on primary pod
func AssertCreateTestData(namespace, clusterName, tableName string) {
	By("creating test data", func() {
		primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).NotTo(HaveOccurred())
		commandTimeout := time.Second * 5
		query := fmt.Sprintf("CREATE TABLE %v AS VALUES (1), (2);", tableName)
		_, _, err = env.ExecCommand(env.Ctx, *primaryPodInfo, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// AssertTestDataExistence verifies the test data exists on the given pod
func AssertTestDataExistence(namespace, podName, tableName string) {
	By(fmt.Sprintf("verifying test data on pod %v", podName), func() {
		newPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		Pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, newPodNamespacedName, Pod)
		Expect(err).ToNot(HaveOccurred())
		query := fmt.Sprintf("select count(*) from %v", tableName)
		commandTimeout := time.Second * 5
		// The data previously created should be there
		stdout, _, err := env.ExecCommand(env.Ctx, *Pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Trim(stdout, "\n"), err).To(BeEquivalentTo("2"))
	})
}

// assertClusterStandbysAreStreaming verifies that all the standbys of a
// cluster have a wal receiver running.
func assertClusterStandbysAreStreaming(namespace string, clusterName string) {
	Eventually(func() error {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		if err != nil {
			return err
		}

		primary, err := env.GetClusterPrimary(namespace, clusterName)
		if err != nil {
			return err
		}

		for _, pod := range podList.Items {
			// Primary should be ignored
			if pod.GetName() == primary.GetName() {
				continue
			}

			timeout := time.Second
			out, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &timeout,
				"psql", "-U", "postgres", "-tAc", "SELECT count(*) FROM pg_stat_wal_receiver")
			if err != nil {
				return err
			}

			value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
			if atoiErr != nil {
				return atoiErr
			}
			if value != 1 {
				return fmt.Errorf("pod %v not streaming", pod.Name)
			}
		}

		return nil
	}, 60).ShouldNot(HaveOccurred())
}
