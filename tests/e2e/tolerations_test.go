/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we check that the operator is able to failover primary and brings back
// replicas when we drain node
var _ = Describe("E2E Tolerations Node", Serial, Label(tests.LabelDisruptive), func() {
	var taintedNodes []string
	namespace := "test-tolerations"
	const (
		sampleFile    = fixturesDir + "/tolerations/cluster-tolerations.yaml"
		clusterName   = "cluster-tolerations"
		tolerationKey = "test-tolerations"
		level         = tests.Lowest
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
		_ = env.DeleteNamespace(namespace)
		for _, node := range taintedNodes {
			cmd := fmt.Sprintf("kubectl taint node %v %s=test:NoSchedule-", node, tolerationKey)
			_, _, err := utils.Run(cmd)
			Expect(err).ToNot(HaveOccurred())
		}
		taintedNodes = nil
	})

	It("can create a cluster with tolerations", func() {
		// Initialize empty global namespace variable
		err := env.CreateNamespace(namespace)
		Expect(err).To(BeNil())

		By("tainting all the nodes", func() {
			nodes, _ := env.GetNodeList()
			// We taint all the nodes where we could run the workloads
			for _, node := range nodes.Items {
				if (node.Spec.Unschedulable != true) && (len(node.Spec.Taints) == 0) {
					cmd := fmt.Sprintf("kubectl taint node %v %s=test:NoSchedule", node.Name, tolerationKey)
					_, _, err := utils.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
					taintedNodes = append(taintedNodes, node.Name)
				}
			}
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
	})
})
