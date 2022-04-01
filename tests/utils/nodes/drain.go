/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package nodes contains the helper methods/functions for nodes
package nodes

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// DrainPrimaryNode drains the node containing the primary pod.
// It returns the names of the pods that were running on that node
func DrainPrimaryNode(namespace string, clusterName string, env *utils.TestingEnvironment) []string {
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
		timeout := 900
		// should set a timeout otherwise will hang forever
		var stdout, stderr string
		Eventually(func() error {
			cmd := fmt.Sprintf("kubectl drain %v --ignore-daemonsets --delete-local-data --force --timeout=%ds",
				primaryNode, timeout)
			stdout, stderr, err = utils.RunUnchecked(cmd)
			return err
		}, timeout).ShouldNot(HaveOccurred(), fmt.Sprintf("stdout: %s, stderr: %s", stdout, stderr))
	})
	By("ensuring no cluster pod is still running on the drained node", func() {
		timeout := 60
		Eventually(func() ([]string, error) {
			var usedNodes []string
			podList, err := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				usedNodes = append(usedNodes, pod.Spec.NodeName)
			}
			return usedNodes, err
		}, timeout).ShouldNot(ContainElement(primaryNode))
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
