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
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Operator High Availability", Serial,
	Label(tests.LabelDisruptive, tests.LabelOperator), func() {
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
			if !MustGetEnvProfile().IsLeaderElectionEnabled() {
				Skip("Skip the scale test case if leader election is disabled")
			}
		})

		It("can work as HA mode", func() {
			// Make sure there's at least one pod of the operator
			err := operator.ScaleOperatorDeployment(env.Ctx, env.Client, 1)
			Expect(err).ToNot(HaveOccurred())

			// Get Operator Pod name
			operatorPodName, err := operator.GetPod(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())

			By("having an operator already running", func() {
				// Waiting for the Operator to be up and running
				Eventually(func() (bool, error) {
					return utils.IsPodReady(operatorPodName), err
				}, 120).Should(BeTrue())
			})

			// Get operator namespace
			operatorNamespace, err := operator.NamespaceName(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())

			// Create the cluster namespace
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			// Create Cluster
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying current leader", func() {
				// Check for the current Operator Pod leader from ConfigMap
				Expect(operator.GetLeaderInfoFromLease(
					env.Ctx, env.Interface,
					operatorNamespace)).To(HavePrefix(operatorPodName.GetName()))
			})

			By("scale up operator replicas to 3", func() {
				// Set old leader pod name to operator pod name
				oldLeaderPodName = operatorPodName.GetName()

				err := operator.ScaleOperatorDeployment(env.Ctx, env.Client, 3)
				Expect(err).ToNot(HaveOccurred())

				// Gather pod names from operator deployment
				podList, err := podutils.List(env.Ctx, env.Client, operatorNamespace)
				Expect(err).ToNot(HaveOccurred())
				for _, podItem := range podList.Items {
					operatorPodNames = append(operatorPodNames, podItem.GetName())
				}
			})

			By("verifying leader information after scale up", func() {
				// Check for Operator Pod leader from ConfigMap to be the former one
				Eventually(func() (string, error) {
					return operator.GetLeaderInfoFromLease(
						env.Ctx, env.Interface,
						operatorNamespace)
				}, 60).Should(HavePrefix(oldLeaderPodName))
			})

			By("deleting current leader", func() {
				// Force delete former Operator leader Pod
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, operatorNamespace, oldLeaderPodName, quickDelete)
				Expect(err).ToNot(HaveOccurred())

				// Verify operator pod should have been deleted
				Eventually(func() []string {
					podList, err := podutils.List(env.Ctx, env.Client, operatorNamespace)
					Expect(err).ToNot(HaveOccurred())
					podNames := make([]string, 0, len(podList.Items))
					for _, podItem := range podList.Items {
						podNames = append(podNames, podItem.GetName())
					}
					return podNames
				}, 120).ShouldNot(ContainElement(oldLeaderPodName))
			})

			By("new leader should be configured", func() {
				// Verify that the leader name is different from the previous one
				Eventually(func() (string, error) {
					return operator.GetLeaderInfoFromLease(
						env.Ctx, env.Interface,
						operatorNamespace)
				}, 120).ShouldNot(HavePrefix(oldLeaderPodName))
			})

			By("verifying reconciliation", func() {
				// Get current CNPG cluster's Primary
				currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				oldPrimary := currentPrimary.GetName()

				// Force-delete the primary
				quickDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &quickDeletionPeriod,
				}
				err = podutils.Delete(env.Ctx, env.Client, namespace, currentPrimary.GetName(), quickDelete)
				Expect(err).ToNot(HaveOccurred())

				// Expect a new primary to be elected and promoted
				AssertNewPrimary(namespace, clusterName, oldPrimary)
			})

			By("scale down operator replicas to 1", func() {
				// Scale down operator deployment to one replica
				err := operator.ScaleOperatorDeployment(env.Ctx, env.Client, 1)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying leader information after scale down", func() {
				// Get Operator Pod name
				operatorPodName, err := operator.GetPod(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())

				// Verify the Operator Pod is the leader
				Eventually(func() (string, error) {
					return operator.GetLeaderInfoFromLease(
						env.Ctx, env.Interface,
						operatorNamespace)
				}, 120).Should(HavePrefix(operatorPodName.GetName()))
			})
		})
	})
