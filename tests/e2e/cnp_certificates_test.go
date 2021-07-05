/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"time"

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

// - default behavior is covered by all the other tests, so there is no need to implement it
// - spinning up a cluster with defaults and switching to a provided certificate should work, implemented here
// - spinning up a new cluster with a given CA and TLS secret should work, implemented here

// Set of tests in which we check that we are able to connect to the cluster
// from an application, by using certificates that have been created by 'kubectl-cnp'
// Then we verify that the server certificate  and the operator are able to handle the provided server certificates
var _ = Describe("Certificates", func() {
	const caSecName = "my-postgresql-server-ca"
	const tlsSecName = "my-postgresql-server"
	const sampleAppFileUserSuppliedCert = fixturesDir + "/cnp_certificates/02-app-pod-user-supplied-cert-secrets.yaml"
	const appPodUserSuppliedCert = "app-pod-user-supplied-cert"

	Context("Operator managed mode", func() {
		const namespace = "certificates-e2e"
		const clusterName = "postgresql-cert"
		const sampleFile = fixturesDir + "/cnp_certificates/cluster-ssl-enabled.yaml"
		const sampleAppFile = fixturesDir + "/cnp_certificates/01-app-pod-cert-secrets.yaml"
		const appPod = "app-pod"

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

		It("can authenticate using a Certificate that is generated from the 'kubectl-cnp' plugin", func() {
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			AssertClientCertificatesSecrets(namespace, clusterName)

			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFile, appPod)

			AssertServerCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName)

			By("switching to user-supplied server certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e server CA and TLS secrets inside the cluster
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl patch cluster %v -n %v -p '{\"spec\":{\"certificates\":{\"serverCASecret\":\"%v\"}}}'"+
						" --type='merge'", clusterName, namespace, caSecName))
				Expect(err).ToNot(HaveOccurred())
				_, _, err = tests.Run(fmt.Sprintf(
					"kubectl patch cluster %v -n %v "+
						"-p '{\"spec\":{\"certificates\":{\"serverTLSSecret\":\"%v\"}}}' --type='merge'",
					clusterName, namespace, tlsSecName))
				Expect(err).ToNot(HaveOccurred())

				// Check that both server CA and TLS secrets have been modified inside cluster status
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				Eventually(func() (bool, error) {
					certUpdateStatus := false
					cluster := &apiv1.Cluster{}
					err = env.Client.Get(env.Ctx, namespacedName, cluster)
					if cluster.Status.Certificates.ServerCASecret == caSecName {
						if cluster.Status.Certificates.ServerTLSSecret == tlsSecName {
							certUpdateStatus = true
						}
					}
					return certUpdateStatus, err
				}, 120).Should(BeTrue(), fmt.Sprintf("Error: %v", err))
			})

			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFileUserSuppliedCert, appPodUserSuppliedCert)
		})
	})
	Context("User supplied server certificate mode", func() {
		const sampleFile = fixturesDir + "/cnp_certificates/cluster-user-supplied-certificates.yaml"
		const namespace = "server-certificates-e2e"
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

		It("can authenticate using a Certificate that is  generated from the 'kubectl-cnp' plugin "+
			"and verify-ca the provided server certificate", func() {
			// Create a cluster in a namespace that will be deleted after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertServerCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName)
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertClientCertificatesSecrets(namespace, clusterName)
			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFileUserSuppliedCert, appPodUserSuppliedCert)
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
	})
}

func AssertClientCertificatesSecrets(namespace, clusterName string) {
	clientCertName := "cluster-cert"
	By("creating a client Certificate using the 'kubectl-cnp' plugin", func() {
		_, _, err := tests.Run(fmt.Sprintf(
			"kubectl cnp certificate %v --cnp-cluster %v --cnp-user app -n %v",
			clientCertName,
			clusterName,
			namespace))
		Expect(err).ToNot(HaveOccurred())
	})
	By("verifying client certificate secret", func() {
		secret := &corev1.Secret{}
		err := env.Client.Get(env.Ctx, client.ObjectKey{Namespace: namespace, Name: clientCertName}, secret)
		Expect(err).ToNot(HaveOccurred())
	})
}

func AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFile, testPodName string) {
	By("creating an app Pod and connecting to DB, using Certificate authentication", func() {
		_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleAppFile)
		Expect(err).ToNot(HaveOccurred())
		// The pod should be ready
		podNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      testPodName,
		}
		pod := &corev1.Pod{}
		Eventually(func() (bool, error) {
			err = env.Client.Get(env.Ctx, podNamespacedName, pod)
			return utils.IsPodActive(*pod) && utils.IsPodReady(*pod), err
		}, 240).Should(BeTrue())

		// Connecting to DB, using Certificate authentication
		Eventually(func() (string, string, error) {
			dsn := fmt.Sprintf("host=%v-rw.%v.svc port=5432 "+
				"sslkey=/etc/secrets/tls/tls.key "+
				"sslcert=/etc/secrets/tls/tls.crt "+
				"sslrootcert=/etc/secrets/ca/ca.crt "+
				"dbname=app user=app sslmode=verify-full", clusterName, namespace)
			timeout := time.Second * 2
			stdout, stderr, err := env.ExecCommand(env.Ctx, *pod, testPodName, &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			return stdout, stderr, err
		}, 360).Should(BeEquivalentTo("1\n"))
	})
}
