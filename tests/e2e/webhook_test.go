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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*
This test affects the operator itself, so it must be run isolated from the
others. Hence, we are running it as both serial and ordered (we don't want webhook to
be disabled first if they are randomized)
Check if webhook works as expected, then disable webhook and check if default values are
affected.
*/

var _ = Describe("webhook", Serial, Label(tests.LabelDisruptive, tests.LabelOperator), Ordered, func() {
	// Define some constants to be used in the test
	const (
		sampleFile        = fixturesDir + "/base/cluster-storage-class.yaml.template"
		operatorNamespace = "cnpg-system"
		level             = tests.Highest
		mutatingWebhook   = "mcluster.kb.io"
		validatingWebhook = "vcluster.kb.io"
	)

	var webhookNamespace, clusterName string
	var clusterIsDefaulted bool
	var err error

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(webhookNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	BeforeAll(func() {
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
	})

	It("checks if webhook works as expected", func() {
		webhookNamespacePrefix := "webhook-test"
		clusterIsDefaulted = true
		By("having a deployment for the operator in state ready", func() {
			deployment, err := env.GetOperatorDeployment()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Status.ReadyReplicas).Should(BeEquivalentTo(1))
		})

		// Create a basic PG cluster
		webhookNamespace, err = env.CreateUniqueNamespace(webhookNamespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(webhookNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(webhookNamespace)
		})
		AssertCreateCluster(webhookNamespace, clusterName, sampleFile, env)
		// Check if cluster is ready and the default values are populated
		AssertClusterDefault(webhookNamespace, clusterName, clusterIsDefaulted, env)
	})

	It("Does not crash the operator when disabled", func() {
		webhookNamespacePrefix := "no-webhook-test"
		clusterIsDefaulted = true

		mWebhook, admissionNumber, err := utils.GetCNPGsMutatingWebhookByName(env, mutatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		// Add a namespace selector to MutatingWebhooks and ValidatingWebhook, this will assign the webhooks
		// only to one namespace simulating the action of disabling the webhooks
		By(fmt.Sprintf("Disabling the mutating webhook %v namespace", operatorNamespace), func() {
			newWebhook := mWebhook.DeepCopy()
			newWebhook.Webhooks[admissionNumber].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"test": "value"},
			}
			err := utils.UpdateCNPGsMutatingWebhookConf(env, newWebhook)
			Expect(err).ToNot(HaveOccurred())
		})

		vWebhook, admissionNumber, err := utils.GetCNPGsValidatingWebhookByName(env, validatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Disabling the validating webhook %v namespace", operatorNamespace), func() {
			newWebhook := vWebhook.DeepCopy()
			newWebhook.Webhooks[admissionNumber].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"test": "value"},
			}
			err := utils.UpdateCNPGsValidatingWebhookConf(env, newWebhook)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a basic PG cluster
		webhookNamespace, err = env.CreateUniqueNamespace(webhookNamespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(webhookNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(webhookNamespace)
		})
		AssertCreateCluster(webhookNamespace, clusterName, sampleFile, env)
		// Check if cluster is ready and has no default value in the object
		AssertClusterDefault(webhookNamespace, clusterName, clusterIsDefaulted, env)

		// Make sure the operator is intact and not crashing
		By("having a deployment for the operator in state ready", func() {
			deployment, err := env.GetOperatorDeployment()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Status.ReadyReplicas).Should(BeEquivalentTo(1))
		})

		By("by cleaning up the webhook configurations", func() {
			// Removing the namespace selector in the webhooks
			AssertWebhookEnabled(env, mutatingWebhook, validatingWebhook)
		})
	})
})
