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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we test the concurrent disruption of both the primary
// and the operator pods, asserting that the latter is able to perform a pending
// failover once a new operator pod comes back available.
var _ = Describe("Operator unavailable", Serial, Label(tests.LabelDisruptive), func() {
	const (
		clusterName = "operator-unavailable"
		sampleFile  = fixturesDir + "/operator-unavailable/operator-unavailable.yaml.template"
		level       = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("Scale down operator replicas to zero and delete primary", func() {
		const namespace = "op-unavailable-e2e-zero-replicas"
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		It("can survive operator failures", func() {
			// Create the cluster namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Load test data
			currentPrimary := clusterName + "-1"
			AssertCreateTestData(namespace, clusterName, "test")

			operatorNamespace, err := env.GetOperatorNamespaceName()
			Expect(err).ToNot(HaveOccurred())

			By("scaling down operator replicas to zero", func() {
				operatorDeployment, err := env.GetOperatorDeployment()
				Expect(err).ToNot(HaveOccurred())
				// Scale down operator deployment to zero replicas
				cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=0 -n %v",
					operatorDeployment.Name, operatorNamespace)
				_, _, err = testsUtils.Run(cmd)
				Expect(err).ToNot(HaveOccurred())

				// Verify the operator pod is not present anymore
				Eventually(func() (int, error) {
					podList := &corev1.PodList{}
					err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
					return len(podList.Items), err
				}, 120).Should(BeEquivalentTo(0))
			})

			By("deleting primary pod", func() {
				// Force-delete the primary
				zero := int64(0)
				forceDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &zero,
				}
				err = env.DeletePod(namespace, currentPrimary, forceDelete)
				Expect(err).ToNot(HaveOccurred())

				// Expect only 2 instances to be up and running
				Eventually(func() int32 {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList,
						ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{"postgresql": clusterName},
					)
					Expect(err).ToNot(HaveOccurred())
					return int32(len(podList.Items))
				}, 120).Should(BeEquivalentTo(2))

				// And to stay like that
				Consistently(func() int32 {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList,
						ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{"postgresql": clusterName},
					)
					Expect(err).ToNot(HaveOccurred())
					return int32(len(podList.Items))
				}, 10).Should(BeEquivalentTo(2))
			})

			By("scaling up the operator replicas to 1", func() {
				// Scale up operator deployment to one replica
				deployment, err := env.GetOperatorDeployment()
				Expect(err).ToNot(HaveOccurred())
				cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=1 -n %v",
					deployment.Name, operatorNamespace)
				_, _, err = testsUtils.Run(cmd)
				Expect(err).ToNot(HaveOccurred())
				timeout := 120
				Eventually(func() (int, error) {
					podList := &corev1.PodList{}
					err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
					return utils.CountReadyPods(podList.Items), err
				}, timeout).Should(BeEquivalentTo(1))
			})

			// Expect a new primary to be elected and promoted
			AssertNewPrimary(namespace, clusterName, currentPrimary)

			By("expect a standby with the same name of the old primary to be created", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      currentPrimary,
				}
				timeout := 180
				Eventually(func() (bool, error) {
					pod := corev1.Pod{}
					err := env.Client.Get(env.Ctx, namespacedName, &pod)
					return specs.IsPodStandby(pod), err
				}, timeout).Should(BeTrue())
			})
			// Expect the test data previously created to be available
			primary, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertDataExpectedCount(namespace, primary.GetName(), "test", 2)
		})
	})

	Context("Delete primary and operator concurrently", func() {
		const namespace = "op-unavailable-e2e-delete-operator"
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		It("can survive operator failures", func() {
			var operatorPodName string
			// Create the cluster namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Load test data
			currentPrimary := clusterName + "-1"
			AssertCreateTestData(namespace, clusterName, "test")

			operatorNamespace, err := env.GetOperatorNamespaceName()
			Expect(err).ToNot(HaveOccurred())

			By("deleting primary and operator pod", func() {
				// Get operator pod name
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
				Expect(err).ToNot(HaveOccurred())
				operatorPodName = podList.Items[0].ObjectMeta.Name

				// Force-delete the operator and the primary
				zero := int64(0)
				forceDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &zero,
				}

				wg := sync.WaitGroup{}
				wg.Add(1)
				wg.Add(1)
				go func() {
					_ = env.DeletePod(operatorNamespace, operatorPodName, forceDelete)
					wg.Done()
				}()
				go func() {
					_ = env.DeletePod(namespace, currentPrimary, forceDelete)
					wg.Done()
				}()
				wg.Wait()

				// Expect only 2 instances to be up and running
				Eventually(func() int32 {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList, ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{"postgresql": "operator-unavailable"},
					)
					Expect(err).ToNot(HaveOccurred())
					return int32(len(podList.Items))
				}, 120).Should(BeEquivalentTo(2))
			})

			By("verifying the operator pod is now back", func() {
				timeout := 120
				Eventually(func() (int, error) {
					podList := &corev1.PodList{}
					err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
					return utils.CountReadyPods(podList.Items), err
				}, timeout).Should(BeEquivalentTo(1))
				// Check that the new operator pod has been created with a different name
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items[0].ObjectMeta.Name).ShouldNot(BeEquivalentTo(operatorPodName))
			})

			// Expect a new primary to be elected and promoted
			AssertNewPrimary(namespace, clusterName, currentPrimary)

			By("expect a standby with the same name of the old primary to be created", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      currentPrimary,
				}
				timeout := 180
				Eventually(func() (bool, error) {
					pod := corev1.Pod{}
					err := env.Client.Get(env.Ctx, namespacedName, &pod)
					return specs.IsPodStandby(pod), err
				}, timeout).Should(BeTrue())
			})
			// Expect the test data previously created to be available
			primary, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertDataExpectedCount(namespace, primary.GetName(), "test", 2)
		})
	})
})
