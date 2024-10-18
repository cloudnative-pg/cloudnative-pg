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
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// DrainPrimaryNode drains the node containing the primary pod.
// It returns the names of the pods that were running on that node
func DrainPrimaryNode(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName string,
	timeoutSeconds int,
) []string {
	var primaryNode string
	var podNames []string
	By("identifying primary node and draining", func() {
		pod, err := clusterutils.GetClusterPrimary(ctx, crudClient, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primaryNode = pod.Spec.NodeName

		// Gather the pods running on this node
		podList, err := clusterutils.GetClusterPodList(ctx, crudClient, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			if pod.Spec.NodeName == primaryNode {
				podNames = append(podNames, pod.Name)
			}
		}

		// Draining the primary pod's node
		var stdout, stderr string
		Eventually(func() error {
			cmd := fmt.Sprintf("kubectl drain %v --ignore-daemonsets --delete-emptydir-data --force --timeout=%ds",
				primaryNode, timeoutSeconds)
			stdout, stderr, err = run.Unchecked(cmd)
			return err
		}, timeoutSeconds).ShouldNot(HaveOccurred(), fmt.Sprintf("stdout: %s, stderr: %s", stdout, stderr))
	})
	By("ensuring no cluster pod is still running on the drained node", func() {
		Eventually(func() ([]string, error) {
			var usedNodes []string
			podList, err := clusterutils.GetClusterPodList(ctx, crudClient, namespace, clusterName)
			for _, pod := range podList.Items {
				usedNodes = append(usedNodes, pod.Spec.NodeName)
			}
			return usedNodes, err
		}, 60).ShouldNot(ContainElement(primaryNode))
	})

	return podNames
}

// UncordonAllNodes executes the 'kubectl uncordon' command on each node of the list
func UncordonAllNodes(
	ctx context.Context,
	crudClient client.Client,
) error {
	nodeList, err := GetNodeList(ctx, crudClient)
	if err != nil {
		return err
	}
	for _, node := range nodeList.Items {
		command := fmt.Sprintf("kubectl uncordon %v", node.Name)
		_, _, err = run.Run(command)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetNodeList gathers the current list of Nodes
func GetNodeList(
	ctx context.Context,
	crudClient client.Client,
) (*v1.NodeList, error) {
	nodeList := &v1.NodeList{}
	err := crudClient.List(ctx, nodeList, client.InNamespace(""))
	return nodeList, err
}

// DescribeKubernetesNodes prints the `describe node` for each node in the
// kubernetes cluster
func DescribeKubernetesNodes(ctx context.Context, crudClient client.Client) (string, error) {
	nodeList, err := GetNodeList(ctx, crudClient)
	if err != nil {
		return "", err
	}
	var report strings.Builder
	for _, node := range nodeList.Items {
		command := fmt.Sprintf("kubectl describe node %v", node.Name)
		stdout, _, err := run.Run(command)
		if err != nil {
			return "", err
		}
		report.WriteString("================================================\n")
		report.WriteString(stdout)
		report.WriteString("================================================\n")
	}
	return report.String(), nil
}
