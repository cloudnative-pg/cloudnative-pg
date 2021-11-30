/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

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
		level = tests.Low
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

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

			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer, false)

			By("switching to user-supplied server certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e server CA and TLS secrets inside the cluster
				_, _, err := utils.Run(fmt.Sprintf(
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
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient,
				certs.CertTypeClient, false)

			By("switching to user-supplied client certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e client CA and TLS secrets inside the cluster
				_, _, err = utils.Run(fmt.Sprintf(
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
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer, false)
			// Create certificates secret for client
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient,
				certs.CertTypeClient, false)

			By("switching to user-supplied server and client certificates", func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e server and client CA and TLS secrets inside the cluster
				_, _, err := utils.Run(fmt.Sprintf(
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
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer, false)
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
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient,
				certs.CertTypeClient, false)
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
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecName, tlsSecName, certs.CertTypeServer, false)

			// Create certificates secret for client
			CreateAndAssertCertificatesSecrets(namespace, clusterName, caSecNameClient, tlsSecNameClient,
				certs.CertTypeClient, false)
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertDBConnectionFromAppPod(namespace, clusterName, sampleUserSuppliedCertClientServer, appPodUserSuppliedCert)
		})
	})
})
