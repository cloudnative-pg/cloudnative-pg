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

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Operator High Availability", Serial,
	Label(tests.LabelDisruptive, tests.LabelNoOpenshift, tests.LabelOperator), func() {
		const (
			namespacePrefix = "operator-ha-e2e"
			sampleFile      = fixturesDir + "/operator-ha/operator-ha.yaml.template"
			clusterName     = "operator-ha"
			level           = tests.Lowest
		)
		var operatorPodNames []string
		var oldLeaderPodName, namespace string

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		It("can work as HA mode", func() {
			// Get Operator Pod name
			operatorPodName, err := env.GetOperatorPod()
			Expect(err).ToNot(HaveOccurred())

			By("having an operator already running", func() {
				// Waiting for the Operator to be up and running
				Eventually(func() (bool, error) {
					return utils.IsPodReady(operatorPodName), err
				}, 120).Should(BeTrue())
			})

			// Get operator namespace
			operatorNamespace, err := env.GetOperatorNamespaceName()
			Expect(err).ToNot(HaveOccurred())

			// Get operator deployment name
			operatorDeployment, err := env.GetOperatorDeployment()
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster namespace
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})

			// Create Cluster
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying current leader", func() {
				// Check for the current Operator Pod leader from ConfigMap
				Expect(testsUtils.GetLeaderInfoFromLease(operatorNamespace, env)).To(HavePrefix(operatorPodName.GetName()))
			})

			By("scale up operator replicas to 3", func() {
				// Set old leader pod name to operator pod name
				oldLeaderPodName = operatorPodName.GetName()

				// Scale up operator deployment to 3 replicas
				cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=3 -n %v",
					operatorDeployment.Name, operatorNamespace)
				_, _, err = testsUtils.Run(cmd)
				Expect(err).ToNot(HaveOccurred())

				// Verify the 3 operator pods are present
				Eventually(func() (int, error) {
					podList, _ := env.GetPodList(operatorNamespace)
					return utils.CountReadyPods(podList.Items), err
				}, 120).Should(BeEquivalentTo(3))

				// Gather pod names from operator deployment
				podList, err := env.GetPodList(operatorNamespace)
				Expect(err).ToNot(HaveOccurred())
				for _, podItem := range podList.Items {
					operatorPodNames = append(operatorPodNames, podItem.GetName())
				}
			})

			By("verifying leader information after scale up", func() {
				// Check for Operator Pod leader from ConfigMap to be the former one
				Eventually(func() (string, error) {
					return testsUtils.GetLeaderInfoFromLease(operatorNamespace, env)
				}, 60).Should(HavePrefix(oldLeaderPodName))
			})

			By("deleting current leader", func() {
				// Force delete former Operator leader Pod
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = env.DeletePod(operatorNamespace, oldLeaderPodName, quickDelete)
				Expect(err).ToNot(HaveOccurred())

				// Verify operator pod should have been deleted
				Eventually(func() []string {
					podList, err := env.GetPodList(operatorNamespace)
					Expect(err).ToNot(HaveOccurred())
					var podNames []string
					for _, podItem := range podList.Items {
						podNames = append(podNames, podItem.GetName())
					}
					return podNames
				}, 120).ShouldNot(ContainElement(oldLeaderPodName))
			})

			By("new leader should be configured", func() {
				// Verify that the leader name is different from the previous one
				Eventually(func() (string, error) {
					return testsUtils.GetLeaderInfoFromLease(operatorNamespace, env)
				}, 120).ShouldNot(HavePrefix(oldLeaderPodName))
			})

			By("verifying reconciliation", func() {
				// Get current CNPG cluster's Primary
				currentPrimary, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				oldPrimary := currentPrimary.GetName()

				// Force-delete the primary
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = env.DeletePod(namespace, currentPrimary.GetName(), quickDelete)
				Expect(err).ToNot(HaveOccurred())

				// Expect a new primary to be elected and promoted
				AssertNewPrimary(namespace, clusterName, oldPrimary)
			})

			By("scale down operator replicas to 1", func() {
				// Scale down operator deployment to one replica
				cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=1 -n %v",
					operatorDeployment.Name, operatorNamespace)
				_, _, err = testsUtils.Run(cmd)
				Expect(err).ToNot(HaveOccurred())

				// Verify there is only one operator pod
				Eventually(func() (int, error) {
					podList := &corev1.PodList{}
					err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
					return len(podList.Items), err
				}, 120).Should(BeEquivalentTo(1))

				// And to stay like that
				Consistently(func() int32 {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList,
						ctrlclient.InNamespace(operatorNamespace),
					)
					Expect(err).ToNot(HaveOccurred())
					return int32(len(podList.Items))
				}, 10).Should(BeEquivalentTo(1))
			})

			By("verifying leader information after scale down", func() {
				// Get Operator Pod name
				operatorPodName, err := env.GetOperatorPod()
				Expect(err).ToNot(HaveOccurred())

				// Verify the Operator Pod is the leader
				Eventually(func() (string, error) {
					return testsUtils.GetLeaderInfoFromLease(operatorNamespace, env)
				}, 120).Should(HavePrefix(operatorPodName.GetName()))
			})
		})
	})
