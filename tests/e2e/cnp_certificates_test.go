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
// - spinning up a cluster with defaults and switching to a provided client certificates should work, implemented here
// - spinning up a cluster with defaults and switching to a provided client and server certificates should work,
//   implemented here
// - spinning up a new cluster with a given server CA and TLS secret should work, implemented here
// - spinning up a new cluster with a given client CA and TLS secret should work, implemented here
// - spinning up a new cluster with a given client and server CA and TLS secret should work, implemented here

// Set of tests in which we check that we are able to connect to the cluster
// from an application, by using certificates that have been created by 'kubectl-cnp'
// Then we verify that the server certificate  and the operator are able to handle the provided server certificates
var _ = Describe("Certificates", func() {
	const (
		caSecName                           = "my-postgresql-server-ca"
		tlsSecName                          = "my-postgresql-server"
		tlsSecNameClient                    = "my-postgresql-client"
		caSecNameClient                     = "my-postgresql-client-ca"
		fixturesCertificatesDir             = fixturesDir + "/cnp_certificates"
		appPodUserSuppliedCert              = "app-pod-user-supplied-cert"
		sampleAppFileUserSuppliedCert       = fixturesCertificatesDir + "/02-app-pod-user-supplied-cert-secrets.yaml"
		sampleAppFileUserSuppliedCertClient = fixturesCertificatesDir + "/03-app-pod-user-supplied-client-cert-secrets.yaml"
		sampleUserSuppliedCertClientServer  = fixturesCertificatesDir + "/04-app-pod-user-supplied-client-" +
			"server-cert-secrets.yaml"
	)

	Context("Operator managed mode", func() {
		const (
			clusterName   = "postgresql-cert"
			sampleFile    = fixturesCertificatesDir + "/cluster-ssl-enabled.yaml"
			sampleAppFile = fixturesCertificatesDir + "/01-app-pod-cert-secrets.yaml"
			appPod        = "app-pod"
		)
		var namespace string
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can authenticate using a Certificate that is generated from the 'kubectl-cnp' plugin", func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace = "certificates-e2e"
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			AssertClientCertificatesSecretsUsingCnpPlugin(namespace, clusterName)

			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFile, appPod)

			AssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer)

			By("switching to user-supplied server certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e server CA and TLS secrets inside the cluster
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl patch cluster %v -n %v -p "+
						"'{\"spec\":{\"certificates\":{\"serverCASecret\":\"%v\","+
						"\"serverTLSSecret\":\"%v\"}}}'"+
						" --type='merge'", clusterName, namespace, caSecName, tlsSecName))
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

		It("should work after switched client certificates to user-supplied mode", func() {
			namespace = "client-cert-switch-to-custom-e2e"
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Create certificates secret for client
			AssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient, certs.CertTypeClient)

			By("switching to user-supplied client certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e client CA and TLS secrets inside the cluster
				_, _, err = tests.Run(fmt.Sprintf(
					"kubectl patch cluster %v -n %v -p "+
						"'{\"spec\":{\"certificates\":{\"clientCASecret\":\"%v\","+
						"\"replicationTLSSecret\":\"%v\"}}}'"+
						" --type='merge'", clusterName, namespace, caSecNameClient, tlsSecNameClient))
				Expect(err).ToNot(HaveOccurred())

				// Check that both server and client CA and TLS secrets have been modified inside cluster status
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}

				Eventually(func() (bool, error) {
					cluster := &apiv1.Cluster{}
					err := env.Client.Get(env.Ctx, namespacedName, cluster)

					return cluster.Status.Certificates.ClientCASecret == caSecNameClient &&
						cluster.Status.Certificates.ReplicationTLSSecret == tlsSecNameClient, err
				}, 120, 5).Should(BeTrue())
			})

			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFileUserSuppliedCertClient, appPodUserSuppliedCert)
		})

		It("should work after switched both server and client certificates to user-supplied mode", func() {
			namespace = "server-client-cert-switch-to-custom-e2e"
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// Create cluster
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			// Create certificates secret for server
			AssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer)
			// Create certificates secret for client
			AssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient, certs.CertTypeClient)

			By("switching to user-supplied server and client certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e server and client CA and TLS secrets inside the cluster
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl patch cluster %v -n %v -p "+
						"'{\"spec\":{\"certificates\":{\"serverCASecret\":\"%v\","+
						"\"serverTLSSecret\":\"%v\",\"clientCASecret\":\"%v\","+
						"\"replicationTLSSecret\":\"%v\"}}}'"+
						" --type='merge'", clusterName, namespace, caSecName, tlsSecName, caSecNameClient, tlsSecNameClient))
				Expect(err).ToNot(HaveOccurred())

				// Check that both server and client CA and TLS secrets have been modified inside cluster status
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}

				Eventually(func() (bool, error) {
					cluster := &apiv1.Cluster{}
					err := env.Client.Get(env.Ctx, namespacedName, cluster)

					return cluster.Status.Certificates.ServerCASecret == caSecName &&
						cluster.Status.Certificates.ClientCASecret == caSecNameClient &&
						cluster.Status.Certificates.ServerTLSSecret == tlsSecName &&
						cluster.Status.Certificates.ReplicationTLSSecret == tlsSecNameClient, err
				}, 120, 5).Should(BeTrue())
			})

			AssertDBConnectionFromAppPod(namespace, clusterName, sampleUserSuppliedCertClientServer, appPodUserSuppliedCert)
		})
	})
	Context("User supplied server certificate mode", func() {
		const (
			sampleFile  = fixturesCertificatesDir + "/cluster-user-supplied-certificates.yaml"
			namespace   = "server-certificates-e2e"
			clusterName = "postgresql-server-cert"
		)
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can authenticate using a Certificate that is generated from the 'kubectl-cnp' plugin "+
			"and verify-ca the provided server certificate", func() {
			// Create a cluster in a namespace that will be deleted after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer)
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertClientCertificatesSecretsUsingCnpPlugin(namespace, clusterName)
			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFileUserSuppliedCert, appPodUserSuppliedCert)
		})
	})

	Context("User supplied client certificate mode", func() {
		const (
			sampleFile  = fixturesCertificatesDir + "/cluster-user-supplied-client-certificates.yaml"
			namespace   = "client-certificates-e2e"
			clusterName = "postgresql-cert"
		)

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can authenticate custom CA to verify client certificates for a cluster", func() {
			// Create a cluster in a namespace that will be deleted after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// Create certificates secret for client
			AssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient, certs.CertTypeClient)
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertDBConnectionFromAppPod(namespace, clusterName, sampleAppFileUserSuppliedCertClient, appPodUserSuppliedCert)
		})
	})

	Context("User supplied both client and server certificate mode", func() {
		const (
			sampleFile  = fixturesCertificatesDir + "/cluster-user-supplied-client-server-certificates.yaml"
			namespace   = "client-server-certificates-e2e"
			clusterName = "postgresql-client-server-cert"
		)

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can authenticate custom CA to verify both client and server certificates for a cluster", func() {
			// Create a cluster in a namespace that will be deleted after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())

			// Create certificates secret for server
			AssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer)

			// Create certificates secret for client
			AssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient, certs.CertTypeClient)
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertDBConnectionFromAppPod(namespace, clusterName, sampleUserSuppliedCertClientServer, appPodUserSuppliedCert)
		})
	})
})

func AssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certType string) {
	// creating root CA certificates
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

	if certType == certs.CertTypeServer {
		By("creating server TLS certificate", func() {
			serverPair, err := caPair.CreateAndSignPair(cluster.GetServiceReadWriteName(), certs.CertTypeServer,
				cluster.GetClusterAltDNSNames(),
			)
			Expect(err).ToNot(HaveOccurred())
			serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
			err = env.Client.Create(env.Ctx, serverSecret)
			Expect(err).ToNot(HaveOccurred())
		})
	}
	if certType == certs.CertTypeClient {
		By("creating client TLS certificate", func() {
			// Sign tls certificates for streaming_replica user
			serverPair, err := caPair.CreateAndSignPair("streaming_replica", certs.CertTypeClient, nil)
			Expect(err).ToNot(HaveOccurred())

			serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
			err = env.Client.Create(env.Ctx, serverSecret)
			Expect(err).ToNot(HaveOccurred())

			// Creating 'app' user tls certificates to validate connection from psql client
			serverPair, err = caPair.CreateAndSignPair("app", certs.CertTypeClient, nil)
			Expect(err).ToNot(HaveOccurred())

			serverSecret = serverPair.GenerateCertificateSecret(namespace, "app-user-cert")
			err = env.Client.Create(env.Ctx, serverSecret)
			Expect(err).ToNot(HaveOccurred())
		})
	}
}

func AssertClientCertificatesSecretsUsingCnpPlugin(namespace, clusterName string) {
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
