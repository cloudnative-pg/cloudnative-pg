/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package sequential

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Set of tests in which we check that the operator is able to failover primary and brings back
// replicas when we drain node
var _ = Describe("E2E Tolerations Node", func() {
	var taintedNodes []string
	namespace := "test-tolerations"
	const sampleFile = fixturesDir + "/tolerations/cluster-example.yaml"
	const clusterName = "cluster-example"
	const tolerationKey = "test-tolerations"

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})

	AfterEach(func() {
		_ = env.DeleteNamespace(namespace)
		for _, node := range taintedNodes {
			cmd := fmt.Sprintf("kubectl taint node %v %s=test:NoSchedule-", node, tolerationKey)
			_, _, err := tests.Run(cmd)
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
					_, _, err := tests.Run(cmd)
					Expect(err).ToNot(HaveOccurred())
					taintedNodes = append(taintedNodes, node.Name)
				}
			}
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
	})
})
