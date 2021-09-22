/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("nodeSelector", func() {
	Context("The label doesn't exists", func() {
		const namespace = "nodeselector-e2e-missing-label"
		const sampleFile = fixturesDir + "/nodeselector/nodeselector-label-not-exists.yaml"
		const clusterName = "postgresql-nodeselector-none-label"
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
		It("verifies that pods can't be scheduled", func() {
			// We create a namespace and verify it exists
			By(fmt.Sprintf("having a %v namespace", namespace), func() {
				err := env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())

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

			By(fmt.Sprintf("creating a cluster in the %v namespace", namespace), func() {
				_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleFile)
				Expect(err).ToNot(HaveOccurred())
			})

			// The cluster should be created but the pods shouldn't be scheduled
			// We expect the operator to create the first pod and for that pod
			// to be stuck forever due to affinity issues.
			// We check the error to verify that's the case
			By("verifying that the pods can't be scheduled", func() {
				timeout := 60
				Eventually(func() bool {
					isPending := false
					podList, err := env.GetPodList(namespace)
					Expect(err).ToNot(HaveOccurred())
					if len(podList.Items) > 0 {
						if len(podList.Items[0].Status.Conditions) > 0 {
							if podList.Items[0].Status.Phase == "Pending" && strings.Contains(podList.Items[0].Status.Conditions[0].Message,
								"didn't match") {
								isPending = true
							}
						}
					}
					return isPending
				}, timeout).Should(BeEquivalentTo(true))
			})
		})
	})

	Context("The label exists", func() {
		const namespace = "nodeselector-e2e-existing-label"
		const sampleFile = fixturesDir + "/nodeselector/nodeselector-label-exists.yaml"
		const clusterName = "postgresql-nodeselector"
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

		It("verifies the pods run on the labeled node", func() {
			var nodeName string
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// We label one node with the label we have defined in the cluster
			// YAML definition
			By("labelling a node", func() {
				nodeList, err := env.GetNodeList()
				Expect(err).ToNot(HaveOccurred())

				// We want to label a node that is uncordoned and untainted,
				// so the pods can be scheduled
				for _, nodeDetails := range nodeList.Items {
					if (nodeDetails.Spec.Unschedulable != true) &&
						(len(nodeDetails.Spec.Taints) == 0) {
						nodeName = nodeDetails.ObjectMeta.Name
						break
					}
				}
				cmd := fmt.Sprintf("kubectl label node %v nodeselectortest=exists --overwrite", nodeName)
				_, _, err = tests.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
			})

			// All the pods should be running on the labeled node
			By("confirm pods run on the labelled node", func() {
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
				podList, err := env.GetPodList(namespace)
				Expect(err).ToNot(HaveOccurred())
				for _, podDetails := range podList.Items {
					if podDetails.Status.Phase == "Running" {
						Expect(podDetails.Spec.NodeName == nodeName).Should(Equal(true))
					}
				}
			})
		})
	})
})
