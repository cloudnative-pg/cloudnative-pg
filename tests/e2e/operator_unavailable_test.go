/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package e2e

import (
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we test the concurrent disruption of both the primary
// and the operator pods, asserting that the latter is able to perform a pending
// failover once a new operator pod comes back available.
var _ = Describe("Operator unavailable", Serial, Label(tests.LabelDisruptive, tests.LabelOperator), func() {
	const (
		clusterName = "operator-unavailable"
		sampleFile  = fixturesDir + "/operator-unavailable/operator-unavailable.yaml.template"
		level       = tests.Medium
	)
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !MustGetEnvProfile().IsLeaderElectionEnabled() {
			Skip("Leader election is disabled")
		}
	})

	Context("Scale down operator replicas to zero and delete primary", func() {
		const namespacePrefix = "op-unavailable-e2e-zero-replicas"
		It("can survive operator failures", func() {
			var err error
			// Create the cluster namespace
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Load test data
			currentPrimary := clusterName + "-1"
			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "test",
			}
			AssertCreateTestData(env, tableLocator)

			By("scaling down operator replicas to zero", func() {
				err := operator.ScaleOperatorDeployment(env.Ctx, env.Client, 0)
				Expect(err).ToNot(HaveOccurred())
			})

			By("deleting primary pod", func() {
				// Force-delete the primary
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, namespace, currentPrimary, quickDelete)
				Expect(err).ToNot(HaveOccurred())

				// Expect only 2 instances to be up and running
				Eventually(func(g Gomega) {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList,
						ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{utils.ClusterLabelName: clusterName},
					)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(podList.Items).To(HaveLen(2))
				}, 120).Should(Succeed())

				// And to stay like that
				Consistently(func() int32 {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList,
						ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{utils.ClusterLabelName: clusterName},
					)
					Expect(err).ToNot(HaveOccurred())
					return int32(len(podList.Items))
				}, 10).Should(BeEquivalentTo(2))
			})

			By("scaling up the operator replicas to 1", func() {
				// Scale up operator deployment to one replica
				err := operator.ScaleOperatorDeployment(env.Ctx, env.Client, 1)
				Expect(err).ToNot(HaveOccurred())
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
			AssertDataExpectedCount(env, tableLocator, 2)
		})
	})

	Context("Delete primary and operator concurrently", func() {
		const namespacePrefix = "op-unavailable-e2e-delete-operator"

		It("can survive operator failures", func() {
			var operatorPodName string
			var err error
			// Create the cluster namespace
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Load test data
			currentPrimary := clusterName + "-1"
			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    "test",
			}
			AssertCreateTestData(env, tableLocator)

			operatorNamespace, err := operator.NamespaceName(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())

			By("deleting primary and operator pod", func() {
				// Get operator pod name
				operatorPodName, err = operator.GetPodName(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())

				// Force-delete the operator and the primary
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}

				wg := sync.WaitGroup{}
				wg.Add(1)
				wg.Go(func() {
					_ = podutils.Delete(env.Ctx, env.Client, operatorNamespace, operatorPodName, quickDelete)
				})
				go func() {
					_ = podutils.Delete(env.Ctx, env.Client, namespace, currentPrimary, quickDelete)
					wg.Done()
				}()
				wg.Wait()

				// Expect only 2 instances to be up and running
				Eventually(func(g Gomega) {
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList, ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{utils.ClusterLabelName: "operator-unavailable"},
					)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(utils.FilterActivePods(podList.Items)).To(HaveLen(2))
				}, 120).Should(Succeed())
			})

			By("verifying a new operator pod is now back", func() {
				timeout := 240
				Eventually(func(g Gomega) {
					operatorPod, err := operator.GetPod(env.Ctx, env.Client)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(operatorPod.Name).NotTo(BeEquivalentTo(operatorPodName))
					g.Expect(operator.IsReady(env.Ctx, env.Client, true)).To(BeTrue())
					g.Expect(operator.PodRestarted(operatorPod)).To(BeFalse(),
						"operator pod should not have any container restarts")
				}, timeout).Should(Succeed())
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
			AssertDataExpectedCount(env, tableLocator, 2)

			// There is a chance that the webhook is not able to reach the new operator pod yet.
			// This could make following tests fail, so we need to wait for the webhook to be working again.
			By("verifying the webhook is working again", func() {
				invalidCluster := &apiv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "invalid"},
					Spec:       apiv1.ClusterSpec{Instances: 1},
				}
				Eventually(func(g Gomega) {
					err := env.Client.Create(env.Ctx, invalidCluster)
					g.Expect(errors.IsInvalid(err)).To(BeTrue())
					g.Expect(err).To(MatchError(ContainSubstring("spec.storage.size")))
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
		})
	})
})
