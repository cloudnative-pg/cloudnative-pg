/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	v13 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
)

/*
This test affects the operator itself, so it must be run isolated from the
others. Hence, we are running it as both serial and ordered (we don't want webhook to
be disabled first if they are randomized)
Check if webhook works as expected, then disable webhook and check if default values are
affected.
*/

var _ = Describe("webhook", Serial, Label(tests.LabelDisruptive), Ordered, func() {
	// Define some constants to be used in the test
	const (
		sampleFile        = fixturesDir + "/base/cluster-storage-class.yaml"
		operatorNamespace = "postgresql-operator-system"
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
			env.DumpClusterEnv(webhookNamespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(webhookNamespace)
		Expect(err).ToNot(HaveOccurred())
	})

	BeforeAll(func() {
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
	})

	It("checks if webhook works as expected", func() {
		webhookNamespace = "webhook-test"
		clusterIsDefaulted = true
		By("having a deployment for the operator in state ready", func() {
			deployment, err := env.GetOperatorDeployment()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Status.ReadyReplicas).Should(BeEquivalentTo(1))
		})

		// Create a basic PG cluster
		err := env.CreateNamespace(webhookNamespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(webhookNamespace, clusterName, sampleFile, env)
		// Check if cluster is ready and the default values are populated
		AssertClusterDefault(webhookNamespace, clusterName, clusterIsDefaulted, env)
	})

	It("Does not crash the operator when disabled", func() {
		webhookNamespace = "no-webhook-test"
		clusterIsDefaulted = true

		mWebhook, admissionNumber, err := utils.GetCNPsMutatingWebhookByName(env, mutatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		// Add a namespace selector to MutatingWebhooks and ValidatingWebhook, this will assign the webhooks
		// only to one namespace simulating the action of disabling the webhooks
		By(fmt.Sprintf("Disabling the mutating webhook %v namespace", operatorNamespace), func() {
			newWebhook := mWebhook.DeepCopy()
			newWebhook.Webhooks[admissionNumber].NamespaceSelector = &v13.LabelSelector{
				MatchLabels: map[string]string{"test": "value"},
			}
			err := utils.UpdateCNPsMutatingWebhookConf(env, newWebhook)
			Expect(err).ToNot(HaveOccurred())
		})

		vWebhook, admissionNumber, err := utils.GetCNPsValidatingWebhookByName(env, validatingWebhook)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Disabling the validating webhook %v namespace", operatorNamespace), func() {
			newWebhook := vWebhook.DeepCopy()
			newWebhook.Webhooks[admissionNumber].NamespaceSelector = &v13.LabelSelector{
				MatchLabels: map[string]string{"test": "value"},
			}
			err := utils.UpdateCNPsValidatingWebhookConf(env, newWebhook)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a basic PG cluster
		err = env.CreateNamespace(webhookNamespace)
		Expect(err).ToNot(HaveOccurred())
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
