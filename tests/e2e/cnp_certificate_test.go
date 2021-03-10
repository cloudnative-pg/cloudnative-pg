/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Set of tests in which we check that we're able to connect to the cluster
// from an application by using certificates created by kubectl-cnp
var _ = Describe("Certificate for tls authentication", func() {
	const namespace = "certificate-e2e"
	const sampleFile = fixturesDir + "/cnp_certificate/cluster-example-ssl-enabled.yaml"
	const sampleAppFile = fixturesDir + "/cnp_certificate/app-pod.yaml"
	const clusterName = "postgresql-cert"
	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})
	It("Can authenticate using a Certificate generated from the kubectl-cnp plugin", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("creating a Certificate using the kubectl-cnp plugin", func() {
			_, _, err = tests.Run(fmt.Sprintf(
				"kubectl cnp certificate cluster-cert --cnp-cluster %v --cnp-user app -n %v",
				clusterName,
				namespace))
			Expect(err).ToNot(HaveOccurred())
		})

		By(fmt.Sprintf("creating an app Pod in the %v namespace", namespace), func() {
			_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleAppFile)
			Expect(err).ToNot(HaveOccurred())
			_, _, podErr := tests.Run(fmt.Sprintf(
				"kubectl wait --for=condition=Ready --timeout=300s pod cert-test -n %v",
				namespace))
			Expect(podErr).ToNot(HaveOccurred())
		})

		By("connecting to DB using Certificate authentication", func() {
			cmd := fmt.Sprintf("psql postgres://app@%v-rw:5432/app?sslmode=verify-ca -tAc 'select 1'",
				clusterName)
			stdout, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v cert-test -- %v",
				namespace,
				cmd))
			Expect(err).ToNot(HaveOccurred())
			Expect(stdout, err).To(Equal("1\n"))
		})

	})
})
