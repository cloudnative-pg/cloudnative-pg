/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package sequential

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Set of tests in which we test the concurrent disruption of both the primary
// and the operator pods, asserting that the latter is able to perform a pending
// failover once a new operator pod comes back available.
var _ = Describe("Operator unavailable", func() {
	const clusterName = "operator-unavailable"
	const sampleFile = fixturesDir + "/operator-unavailable/operator-unavailable.yaml"

	Context("Scale down operator replicas to zero and delete primary", func() {
		const namespace = "op-unavailable-e2e-zero-replicas"
		JustAfterEach(func() {
			if CurrentGinkgoTestDescription().Failed {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentGinkgoTestDescription().FullTestText+".log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})
		It("can survive operator failures", func() {
			// Create the cluster namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Load test data
			currentPrimary := clusterName + "-1"
			AssertTestDataCreation(namespace, clusterName)

			operatorNamespace, err := env.GetOperatorNamespaceName()
			Expect(err).ToNot(HaveOccurred())

			By("scaling down operator replicas to zero", func() {
				operatorDeployment, err := env.GetOperatorDeployment()
				Expect(err).ToNot(HaveOccurred())
				// Scale down operator deployment to zero replicas
				cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=0 -n %v",
					operatorDeployment.Name, operatorNamespace)
				_, _, err = tests.Run(cmd)
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
				_, _, err = tests.Run(cmd)
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
			AssertTestData(namespace, clusterName)
		})
	})

	Context("Delete primary and operator concurrently", func() {
		const namespace = "op-unavailable-e2e-delete-operator"
		JustAfterEach(func() {
			if CurrentGinkgoTestDescription().Failed {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentGinkgoTestDescription().FullTestText+".log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})
		It("can survive operator failures", func() {
			var operatorPodName string
			// Create the cluster namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Load test data
			currentPrimary := clusterName + "-1"
			AssertTestDataCreation(namespace, clusterName)

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
			AssertTestData(namespace, clusterName)
		})
	})
})
