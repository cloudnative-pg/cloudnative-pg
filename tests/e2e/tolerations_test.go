/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we check that the operator is able to failover primary and brings back
// replicas when we drain node
var _ = Describe("E2E Tolerations Node", Serial, Label(tests.LabelDisruptive, tests.LabelPodScheduling), func() {
	var taintedNodes []string
	var namespace string
	const namespacePrefix = "test-tolerations"
	const (
		sampleFile    = fixturesDir + "/tolerations/cluster-tolerations.yaml.template"
		clusterName   = "cluster-tolerations"
		tolerationKey = "test-tolerations"
		level         = tests.Lowest
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	AfterEach(func() {
		for _, node := range taintedNodes {
			cmd := fmt.Sprintf("kubectl taint node %v %s=test:NoSchedule-", node, tolerationKey)
			_, _, err := utils.Run(cmd)
			Expect(err).ToNot(HaveOccurred())
		}
		taintedNodes = nil
	})

	It("can create a cluster with tolerations", func() {
		var err error
		// Initialize empty global namespace variable
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).To(BeNil())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

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
