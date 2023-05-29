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

// Package nodes contains the helper methods/functions for nodes
package nodes

import (
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// DrainPrimaryNode drains the node containing the primary pod.
// It returns the names of the pods that were running on that node
func DrainPrimaryNode(
	namespace,
	clusterName string,
	timeoutSeconds int,
	env *utils.TestingEnvironment,
) []string {
	var primaryNode string
	var podNames []string
	By("identifying primary node and draining", func() {
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primaryNode = pod.Spec.NodeName

		// Gather the pods running on this node
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			if pod.Spec.NodeName == primaryNode {
				podNames = append(podNames, pod.Name)
			}
		}

		// Draining the primary pod's node
		var stdout, stderr string
		Eventually(func() error {
			cmd := fmt.Sprintf("kubectl drain %v --ignore-daemonsets --delete-local-data --force --timeout=%ds",
				primaryNode, timeoutSeconds)
			stdout, stderr, err = utils.RunUnchecked(cmd)
			return err
		}, timeoutSeconds).ShouldNot(HaveOccurred(), fmt.Sprintf("stdout: %s, stderr: %s", stdout, stderr))
	})
	By("ensuring no cluster pod is still running on the drained node", func() {
		Eventually(func() ([]string, error) {
			var usedNodes []string
			podList, err := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				usedNodes = append(usedNodes, pod.Spec.NodeName)
			}
			return usedNodes, err
		}, 60).ShouldNot(ContainElement(primaryNode))
	})

	return podNames
}

// UncordonAllNodes executes the 'kubectl uncordon' command on each node of the list
func UncordonAllNodes(env *utils.TestingEnvironment) error {
	nodeList, err := env.GetNodeList()
	if err != nil {
		return err
	}
	for _, node := range nodeList.Items {
		command := fmt.Sprintf("kubectl uncordon %v", node.Name)
		_, _, err = utils.Run(command)
		if err != nil {
			return err
		}
	}
	return nil
}
