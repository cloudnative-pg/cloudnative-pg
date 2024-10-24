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

	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/certificates"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/webapp"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
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
// from an application, by using certificates that have been created by 'kubectl-cnpg'
// Then we verify that the server certificate  and the operator are able to handle the provided server certificates
var _ = Describe("Certificates", func() {
	const (
		serverCASecretName              = "my-postgresql-server-ca" // #nosec
		serverCertSecretName            = "my-postgresql-server"    // #nosec
		replicaCertSecretName           = "my-postgresql-client"    // #nosec
		clientCertSecretName            = "app-user-cert"           // #nosec
		clientCASecretName              = "my-postgresql-client-ca" // #nosec
		defaultCASecretName             = "postgresql-cert-ca"      // #nosec
		kubectlCNPGClientCertSecretName = "cluster-cert"            // #nosec
		fixturesCertificatesDir         = fixturesDir + "/certificates"
		level                           = tests.Low
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	var namespace, clusterName string

	Context("Operator managed mode", Ordered, func() {
		const (
			sampleFile = fixturesCertificatesDir + "/cluster-ssl-enabled.yaml.template"
		)

		cleanClusterCertification := func() {
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				cluster.Spec.Certificates.ServerTLSSecret = ""
				cluster.Spec.Certificates.ServerCASecret = ""
				cluster.Spec.Certificates.ReplicationTLSSecret = ""
				cluster.Spec.Certificates.ClientCASecret = ""
				return env.Client.Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())
		}

		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "postgresql-cert"
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// Create the client certificate
			cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			err = certificates.CreateClientCertificatesViaKubectlPlugin(
				env.Ctx,
				env.Client,
				*cluster,
				kubectlCNPGClientCertSecretName,
				"app",
			)
			Expect(err).ToNot(HaveOccurred())
		})
		AfterEach(func() {
			// deleting root CA certificates
			cleanClusterCertification()
		})

		It("can authenticate using a Certificate that is generated from the 'kubectl-cnpg' plugin",
			Label(tests.LabelPlugin), func() {
				pod := webapp.DefaultWebapp(namespace, "app-pod-cert-1",
					defaultCASecretName, kubectlCNPGClientCertSecretName)
				err := podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
				Expect(err).ToNot(HaveOccurred())
				AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
			})

		It("can authenticate after switching to user-supplied server certs", Label(tests.LabelServiceConnectivity), func() {
			CreateAndAssertServerCertificatesSecrets(
				namespace,
				clusterName,
				serverCASecretName,
				serverCertSecretName,
				false,
			)

			var err error
			// Updating defaults certificates entries with user provided certificates,
			// i.e server CA and TLS secrets inside the cluster
			Eventually(func() error {
				_, _, err = run.Unchecked(fmt.Sprintf(
					"kubectl patch cluster %v -n %v -p "+
						"'{\"spec\":{\"certificates\":{\"serverCASecret\":\"%v\","+
						"\"serverTLSSecret\":\"%v\"}}}'"+
						" --type='merge'", clusterName, namespace, serverCASecretName, serverCertSecretName))
				if err != nil {
					return err
				}
				return nil
			}, 60, 5).Should(BeNil())

			Eventually(func() (bool, error) {
				certUpdateStatus := false
				cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				if cluster.Status.Certificates.ServerCASecret == serverCASecretName {
					if cluster.Status.Certificates.ServerTLSSecret == serverCertSecretName {
						certUpdateStatus = true
					}
				}
				return certUpdateStatus, err
			}, 120).Should(BeTrue(), fmt.Sprintf("Error: %v", err))

			pod := webapp.DefaultWebapp(
				namespace,
				"app-pod-cert-2",
				serverCASecretName,
				kubectlCNPGClientCertSecretName,
			)
			err = podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
			Expect(err).ToNot(HaveOccurred())
			AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
		})

		It("can connect after switching to user-supplied client certificates", Label(tests.LabelServiceConnectivity), func() {
			// Create certificates secret for client
			CreateAndAssertClientCertificatesSecrets(namespace, clusterName, clientCASecretName, replicaCertSecretName,
				clientCertSecretName, false)

			// Updating defaults certificates entries with user provided certificates,
			// i.e client CA and TLS secrets inside the cluster
			Eventually(func() error {
				_, _, err := run.Unchecked(fmt.Sprintf(
					"kubectl patch cluster %v -n %v -p "+
						"'{\"spec\":{\"certificates\":{\"clientCASecret\":\"%v\","+
						"\"replicationTLSSecret\":\"%v\"}}}'"+
						" --type='merge'", clusterName, namespace, clientCASecretName, replicaCertSecretName))
				if err != nil {
					return err
				}
				return nil
			}, 60, 5).Should(BeNil())

			Eventually(func() (bool, error) {
				cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				return cluster.Spec.Certificates.ClientCASecret == clientCASecretName &&
					cluster.Status.Certificates.ReplicationTLSSecret == replicaCertSecretName, err
			}, 120, 5).Should(BeTrue())

			pod := webapp.DefaultWebapp(namespace, "app-pod-cert-3", defaultCASecretName, clientCertSecretName)
			err := podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
			Expect(err).ToNot(HaveOccurred())
			AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
		})

		It("can connect after switching both server and client certificates to user-supplied mode",
			Label(tests.LabelServiceConnectivity), func() {
				// Updating defaults certificates entries with user provided certificates,
				// i.e server and client CA and TLS secrets inside the cluster
				Eventually(func() error {
					_, _, err := run.Unchecked(fmt.Sprintf(
						"kubectl patch cluster %v -n %v -p "+
							"'{\"spec\":{\"certificates\":{\"serverCASecret\":\"%v\","+
							"\"serverTLSSecret\":\"%v\",\"clientCASecret\":\"%v\","+
							"\"replicationTLSSecret\":\"%v\"}}}'"+
							" --type='merge'",
						clusterName,
						namespace,
						serverCASecretName,
						serverCertSecretName,
						clientCASecretName,
						replicaCertSecretName,
					))
					if err != nil {
						return err
					}
					return nil
				}, 60, 5).Should(BeNil())

				Eventually(func() (bool, error) {
					cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
					return cluster.Status.Certificates.ServerCASecret == serverCASecretName &&
						cluster.Status.Certificates.ClientCASecret == clientCASecretName &&
						cluster.Status.Certificates.ServerTLSSecret == serverCertSecretName &&
						cluster.Status.Certificates.ReplicationTLSSecret == replicaCertSecretName, err
				}, 120, 5).Should(BeTrue())

				pod := webapp.DefaultWebapp(namespace, "app-pod-cert-4", serverCASecretName, clientCertSecretName)
				err := podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
				Expect(err).ToNot(HaveOccurred())
				AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
			})
	})

	Context("User supplied server certificate mode", func() {
		const sampleFile = fixturesCertificatesDir + "/cluster-user-supplied-certificates.yaml.template"

		BeforeEach(func() {
			clusterName = "postgresql-server-cert"
		})

		It("can authenticate using a Certificate that is generated from the 'kubectl-cnpg' plugin "+
			"and verify-ca the provided server certificate", Label(tests.LabelPlugin), func() {
			const namespacePrefix = "server-certificates-e2e"

			var err error
			// Create a cluster in a namespace that will be deleted after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			CreateAndAssertServerCertificatesSecrets(
				namespace,
				clusterName,
				serverCASecretName,
				serverCertSecretName,
				false,
			)
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			err = certificates.CreateClientCertificatesViaKubectlPlugin(
				env.Ctx,
				env.Client,
				*cluster,
				kubectlCNPGClientCertSecretName,
				"app",
			)
			Expect(err).ToNot(HaveOccurred())

			pod := webapp.DefaultWebapp(
				namespace,
				"app-pod-cert-2",
				serverCASecretName,
				kubectlCNPGClientCertSecretName,
			)
			err = podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
			Expect(err).ToNot(HaveOccurred())
			AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
		})
	})

	Context("User supplied client certificate mode", func() {
		const sampleFile = fixturesCertificatesDir + "/cluster-user-supplied-client-certificates.yaml.template"

		BeforeEach(func() {
			clusterName = "postgresql-cert"
		})

		It("can authenticate custom CA to verify client certificates for a cluster",
			Label(tests.LabelServiceConnectivity), func() {
				const namespacePrefix = "client-certificates-e2e"

				var err error
				// Create a cluster in a namespace that will be deleted after the test
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// Create certificates secret for client
				CreateAndAssertClientCertificatesSecrets(
					namespace,
					clusterName,
					clientCASecretName,
					replicaCertSecretName,
					clientCertSecretName,
					false,
				)
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
				pod := webapp.DefaultWebapp(namespace, "app-pod-cert-3", defaultCASecretName, clientCertSecretName)
				err = podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
				Expect(err).ToNot(HaveOccurred())
				AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
			})
	})

	Context("User supplied both client and server certificate mode", func() {
		const sampleFile = fixturesCertificatesDir + "/cluster-user-supplied-client-server-certificates.yaml.template"

		BeforeEach(func() {
			clusterName = "postgresql-client-server-cert"
		})

		It("can authenticate custom CA to verify both client and server certificates for a cluster",
			Label(tests.LabelServiceConnectivity), func() {
				const namespacePrefix = "client-server-certificates-e2e"

				// Create a cluster in a namespace that will be deleted after the test
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())

				// Create certificates secret for server
				CreateAndAssertServerCertificatesSecrets(
					namespace,
					clusterName,
					serverCASecretName,
					serverCertSecretName,
					false,
				)

				CreateAndAssertClientCertificatesSecrets(
					namespace,
					clusterName,
					clientCASecretName,
					replicaCertSecretName,
					clientCertSecretName,
					false,
				)
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
				pod := webapp.DefaultWebapp(namespace, "app-pod-cert-4", serverCASecretName, clientCertSecretName)
				err = podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)
				Expect(err).ToNot(HaveOccurred())
				AssertSSLVerifyFullDBConnectionFromAppPod(namespace, clusterName, pod)
			})
	})
})
