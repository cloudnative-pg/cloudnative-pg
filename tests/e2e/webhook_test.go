/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	testsUtils "github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
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
		clusterName            = "cluster-basic"
		sampleFile             = fixturesDir + "/base/cluster-basic.yaml"
		operatorNamespace      = "postgresql-operator-system"
		level                  = tests.Highest
		patchMutatingWebhook   = `kubectl patch mutatingwebhookconfigurations/postgresql-operator-mutating-webhook-configuration -p '{"webhooks":[{"name":"mcluster.kb.io","namespaceSelector":{"matchLabels":{"test":"value"}}}]}'`     //nolint
		patchValidatingWebhook = `kubectl patch validatingwebhookconfigurations/postgresql-operator-validating-webhook-configuration -p '{"webhooks":[{"name":"vcluster.kb.io","namespaceSelector":{"matchLabels":{"test":"value"}}}]}'` //nolint
	)

	var webhookNamespace string
	var clusterIsDefaulted bool

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
		// Delete the Webhooks (validation and mutation)
		By(fmt.Sprintf("Disabling the mutating webhook %v namespace", operatorNamespace), func() {
			_, _, err := testsUtils.Run(patchMutatingWebhook)
			Expect(err).ToNot(HaveOccurred())
		})

		By(fmt.Sprintf("Disabling the validating webhook %v namespace", operatorNamespace), func() {
			_, _, err := testsUtils.Run(patchValidatingWebhook)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a basic PG cluster
		err := env.CreateNamespace(webhookNamespace)
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
			AssertWebhookEnabled(env)
		})
	})
})
