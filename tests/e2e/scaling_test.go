/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
)

var _ = Describe("Cluster scale up and down", func() {
	const (
		namespace   = "cluster-scale-e2e-storage-class"
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml"
		clusterName = "postgresql-storage-class"
		level       = tests.Lowest
	)
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
	It("can scale the cluster size", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Add a node to the cluster and verify the cluster has one more
		// element
		By("adding an instance to the cluster", func() {
			_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 300
			AssertClusterIsReady(namespace, clusterName, timeout, env)
		})

		// Remove a node from the cluster and verify the cluster has one
		// element less
		By("removing an instance from the cluster", func() {
			_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 60
			AssertClusterIsReady(namespace, clusterName, timeout, env)
		})
	})
})
