/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
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
		cluster := &clusterv1alpha1.Cluster{}
		err := env.Client.Get(env.Ctx, namespacedName, cluster)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() (int32, error) {
			cluster := &clusterv1alpha1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			return cluster.Status.ReadyInstances, err
		}, timeout).Should(BeEquivalentTo(cluster.Spec.Instances))
	})
}

// AssertConnection is used if a connection from a pod to a postgresql
// database works
func AssertConnection(host string, user string, dbname string,
	password string, queryingPod corev1.Pod, env *tests.TestingEnvironment) {
	By(fmt.Sprintf("connecting to the %v service as %v", host, user), func() {
		dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v", host, user, dbname, password)
		timeout := time.Second * 2
		stdout, stderr, err := env.ExecCommand(env.Ctx, queryingPod, "postgres", &timeout,
			"psql", dsn, "-tAc", "SELECT 1")
		Expect(stdout, err).To(Equal("1\n"))
		Expect(stderr).To(BeEmpty())
	})
}
