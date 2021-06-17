/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// TODO Possible test cases:
// - default behavior is covered by all the other tests, so no need to implement it
// - spinning up a new cluster with a given CA and TLS secret should work, implemented here
// - spinning up a cluster with defaults and switching to a provided certificate should work, to be implemented

// Set of tests in which we check that we're able to connect to the cluster
// from an application by using certificates created by kubectl-cnp verifying the server certificate
// and the operator is able to handle the provided server certificates
var _ = Describe("TLS server certificate", func() {
	const namespace = "server-certificate-e2e"
	const sampleFile = fixturesDir + "/cnp_server_certificate/cluster-ssl-enabled.yaml"
	const sampleAppFile = fixturesDir + "/cnp_server_certificate/app-pod.yaml"
	const caSecName = "my-postgresql-server-ca"
	const tlsSecName = "my-postgresql-server"
	const clusterName = "postgresql-server-cert"
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
	It("Can authenticate using a Certificate generated from the kubectl-cnp plugin and verify-ca the provided "+
		"server certificate", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		AssertServerCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName)

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
			// The pod should be ready
			timeout := 300
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      "cert-test",
			}
			Eventually(func() (bool, error) {
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, podNamespacedName, pod)
				return utils.IsPodActive(*pod) && utils.IsPodReady(*pod), err
			}, timeout).Should(BeTrue())
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

func AssertServerCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName string) {
	By("creating the server CA and TLS certificate", func() {
		cluster := &apiv1.Cluster{}
		cluster.Namespace = namespace
		cluster.Name = clusterName

		secret := &corev1.Secret{}
		err := env.Client.Get(env.Ctx, client.ObjectKey{Namespace: namespace, Name: caSecName}, secret)
		Expect(err).To(HaveOccurred())

		caPair, err := certs.CreateRootCA(cluster.Name, namespace)
		Expect(err).ToNot(HaveOccurred())

		caSecret := caPair.GenerateCASecret(namespace, caSecName)
		// delete the key from the CA, as it is not needed in this case
		delete(caSecret.Data, certs.CAPrivateKeyKey)
		err = env.Client.Create(env.Ctx, caSecret)
		Expect(err).ToNot(HaveOccurred())

		serverPair, err := caPair.CreateAndSignPair(cluster.GetServiceReadWriteName(), certs.CertTypeServer,
			cluster.GetClusterAltDNSNames(),
		)
		Expect(err).ToNot(HaveOccurred())

		serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
		err = env.Client.Create(env.Ctx, serverSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(err).ToNot(HaveOccurred())
	})
}
