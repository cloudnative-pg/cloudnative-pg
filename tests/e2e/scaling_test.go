/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster scale up and down", func() {
	const namespace = "cluster-scale-e2e-storage-class"
	const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
	const clusterName = "postgresql-storage-class"
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
	It("can scale the cluster size", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Add a node to the cluster and verify the cluster has one more
		// element
		By("adding an instance to the cluster", func() {
			_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 300
			AssertClusterIsReady(namespace, clusterName, timeout, env)
		})

		// Remove a node from the cluster and verify the cluster has one
		// element less
		By("removing an instance from the cluster", func() {
			_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 60
			AssertClusterIsReady(namespace, clusterName, timeout, env)
		})
	})
})
