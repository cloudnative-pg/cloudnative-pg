/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package sequential

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
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

// AssertNewPrimary checks that, during a failover, a new primary
// is being elected and promoted and that write operation succeed
// on this new pod.
func AssertNewPrimary(namespace string, clusterName string, oldprimary string) {
	By("verifying the new primary pod", func() {
		// Gather the primary
		timeout := 120
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		// Wait for the operator to set a new TargetPrimary
		Eventually(func() (string, error) {
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			return cluster.Status.TargetPrimary, err
		}, timeout).ShouldNot(BeEquivalentTo(oldprimary))
		cluster := &apiv1.Cluster{}
		err := env.Client.Get(env.Ctx, namespacedName, cluster)
		newPrimary := cluster.Status.TargetPrimary
		Expect(err).ToNot(HaveOccurred())

		// Expect the chosen pod to eventually become a primary
		namespacedName = types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimary,
		}
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return specs.IsPodPrimary(pod), err
		}, timeout).Should(BeTrue())
	})
	By("verifying write operation on the new primary pod", func() {
		commandTimeout := time.Second * 5
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		// Expect write operation to succeed
		query := "create table test(var1 text)"
		_, _, err = env.ExecCommand(env.Ctx, *pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})
}

func AssertTestDataCreation(namespace string, clusterName string) {
	By("loading test data", func() {
		testData := "test data"
		tableName := "testTable"
		query := fmt.Sprintf("CREATE TABLE %v(var1 text);INSERT INTO %v VALUES ('%v')", tableName, tableName, testData)
		commandTimeout := time.Second * 5

		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		// Create Sample Data
		_, _, err = env.ExecCommand(env.Ctx, *pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})
}

func AssertTestDataExistence(namespace string, clusterName string) {
	By("verifying test data", func() {
		testData := "test data"
		tableName := "testTable"
		query := fmt.Sprintf("SELECT * FROM %v", tableName)
		commandTimeout := time.Second * 5

		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		// The data previously created should be there
		stdout, _, err := env.ExecCommand(env.Ctx, *pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.TrimSpace(stdout)).Should(BeEquivalentTo(testData))
	})
}

func UncordonAllNodes() {
	nodeList, err := env.GetNodeList()
	Expect(err).ToNot(HaveOccurred())
	// uncordoning all nodes
	for _, node := range nodeList.Items {
		command := fmt.Sprintf("kubectl uncordon %v", node.Name)
		_, _, err := tests.Run(command)
		Expect(err).ToNot(HaveOccurred())
	}
}
