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
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBouncer Connections", Label(tests.LabelServiceConnectivity), func() {
	const (
		sampleFile                    = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml.template"
		poolerBasicAuthRWSampleFile   = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		poolerCertificateRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-tls-rw.yaml"
		poolerBasicAuthROSampleFile   = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-ro.yaml"
		poolerCertificateROSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-tls-ro.yaml"
		level                         = tests.Low
	)
	var err error
	var namespace, clusterName string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("no user-defined certificates", Ordered, func() {
		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "pgbouncer-auth-no-user-certs")
			Expect(err).ToNot(HaveOccurred())
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})
		JustAfterEach(func() {
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			DeleteTableUsingPgBouncerService(namespace, clusterName, poolerBasicAuthRWSampleFile, env, primaryPod)
		})

		It("can connect to Postgres via pgbouncer service using basic authentication", func() {
			By("setting up read write type pgbouncer pooler", func() {
				createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
				assertPgBouncerPoolerDeploymentStrategy(namespace, poolerBasicAuthRWSampleFile, "25%", "25%")
			})

			By("setting up read only type pgbouncer pooler", func() {
				createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
				assertPgBouncerPoolerDeploymentStrategy(namespace, poolerBasicAuthROSampleFile, "24%", "24%")
			})

			By("verifying read and write connections using pgbouncer service", func() {
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerBasicAuthRWSampleFile, true)
			})

			By("verifying read connections using pgbouncer service", func() {
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerBasicAuthROSampleFile, false)
			})

			By("executing psql within the pgbouncer pod", func() {
				pod, err := getPgbouncerPod(namespace, poolerBasicAuthRWSampleFile)
				Expect(err).ToNot(HaveOccurred())

				err = runShowHelpInPod(pod)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("can connect to Postgres via pgbouncer service using tls certificates", func() {
			By("setting up read write type pgbouncer pooler", func() {
				createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 1)
			})

			By("setting up read only type pgbouncer pooler", func() {
				createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 1)
			})

			By("verifying read and write connections using pgbouncer service", func() {
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerCertificateRWSampleFile, true)
			})

			By("verifying read connections using pgbouncer service", func() {
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerCertificateROSampleFile, false)
			})
		})

		It("should recreate after deleting pgbouncer pod", func() {
			assertPodIsRecreated(namespace, poolerBasicAuthRWSampleFile)
			By("verifying pgbouncer read write service connections after deleting pod", func() {
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerBasicAuthRWSampleFile, true)
			})

			assertPodIsRecreated(namespace, poolerBasicAuthROSampleFile)
			By("verifying pgbouncer read only service connections after pod deleting", func() {
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerBasicAuthROSampleFile, false)
			})
		})

		It("should recreate after deleting pgbouncer deployment", func() {
			assertDeploymentIsRecreated(namespace, poolerBasicAuthRWSampleFile)
			By("verifying pgbouncer read write service connections after deleting deployment", func() {
				// verify read and write connections after pgbouncer deployment deletion
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerBasicAuthRWSampleFile, true)
			})

			assertDeploymentIsRecreated(namespace, poolerBasicAuthROSampleFile)
			By("verifying pgbouncer read only service connections after deleting deployment", func() {
				// verify read and write connections after pgbouncer deployment deletion
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
					poolerBasicAuthROSampleFile, false)
			})
		})
	})

	Context("user-defined certificates", Ordered, func() {
		const (
			folderPath    = fixturesDir + "/pgbouncer/pgbouncer_user_supplied_certificates/"
			sampleCluster = folderPath + "cluster-user-supplied-certificates.yaml.template"
		)
		const (
			// PostgreSQL certificates
			postgresServerTLS      = "pg-server-cert"
			postgresServerCA       = "pg-server-ca"
			postgresClientCA       = "pg-client-ca"
			postgresReplicationTLS = "pg-client-streaming-replica"
		)

		BeforeAll(func() {
			// Create a cluster in a namespace that will be deleted after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "pgbouncer-separate-certificates")
			Expect(err).ToNot(HaveOccurred())
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sampleCluster)
			Expect(err).ToNot(HaveOccurred())

			// Create client certificate secrets for PostgreSQL
			CreateAndAssertServerCertificatesSecrets(namespace, clusterName, postgresServerCA, postgresServerTLS, true)
			// Create server certificate secrets for PostgreSQL
			CreateAndAssertClientCertificatesSecrets(namespace, clusterName, postgresClientCA, postgresReplicationTLS,
				"app-user-cert", true)

			AssertCreateCluster(namespace, clusterName, sampleCluster, env)
		})

		It("using automatic TLS configuration", func() {
			const (
				samplePoolerRO = folderPath + "pgbouncer-ro.yaml"
				samplePoolerRW = folderPath + "pgbouncer-rw.yaml"
			)
			createAndAssertPgBouncerPoolerIsSetUp(namespace, samplePoolerRW, 1)
			createAndAssertPgBouncerPoolerIsSetUp(namespace, samplePoolerRO, 1)
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName, samplePoolerRW, true)
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName, samplePoolerRO, false)
		})

		It("using manual TLS configuration (verify-full)", func() {
			const (
				samplePoolerVerifyFull = folderPath + "pgbouncer-verify-full.yaml"
				// Pooler certificates
				poolerServerTLS = "pooler-server-cert"
				poolerServerCA  = postgresClientCA
				poolerClientCA  = "pooler-client-ca"
				poolerClientTLS = "pooler-client-cert"
			)

			By("updating pg_hba and pg_ident of the Cluster to allow cert authentication for the app user", func() {
				cluster := &apiv1.Cluster{}
				err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					var err error
					cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.PgHBA = []string{
						"hostssl all app all cert map=pooler",
						"hostssl all pgbouncer all cert",
					}
					cluster.Spec.PostgresConfiguration.PgIdent = []string{"pooler pgbouncer app"}
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating pooler and its certificates", func() {
				poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, samplePoolerVerifyFull)
				Expect(err).ToNot(HaveOccurred())
				// Create server certificate secrets for Pooler
				createPoolerServerCertificateSecret(namespace, poolerServerCA, poolerServerTLS)
				// Create client certificate secrets for Pooler
				createPoolerClientCertificateSecret(namespace, poolerName, poolerClientCA, poolerClientTLS, true)

				createAndAssertPgBouncerPoolerIsSetUp(namespace, samplePoolerVerifyFull, 1)
			})

			By("connecting to the pooler using mTLS", func() {
				caCertPath, clientCertPath, clientKeyPath := createAppClientCertificates(namespace, testsUtils.AppUser,
					poolerClientCA)
				connectionParams := map[string]string{
					"sslkey":      clientKeyPath,
					"sslcert":     clientCertPath,
					"sslrootcert": caCertPath,
					"sslmode":     "verify-full",
				}
				assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName, samplePoolerVerifyFull,
					true, connectionParams)
			})
		})
	})
})

func getPgbouncerPod(namespace, sampleFile string) (*corev1.Pod, error) {
	poolerKey, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
	if err != nil {
		return nil, err
	}

	Expect(err).ToNot(HaveOccurred())

	var podList corev1.PodList
	err = env.Client.List(env.Ctx, &podList, ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerKey})
	Expect(err).ToNot(HaveOccurred())
	Expect(len(podList.Items)).Should(BeEquivalentTo(1))
	return &podList.Items[0], nil
}

func runShowHelpInPod(pod *corev1.Pod) error {
	_, _, err := exec.Command(
		env.Ctx, env.Interface, env.RestClientConfig, *pod,
		"pgbouncer", nil, "psql", "-c", "SHOW HELP",
	)
	return err
}

// createPoolerServerCertificateSecret fetches an existing PG server CA secret and creates a new certificate
// secret for the pooler using that CA.
// The pooler uses this certificate to authenticate with the PostgreSQL server.
func createPoolerServerCertificateSecret(namespace, sourcePostgresServerCASecretName, secretName string) {
	secret := &corev1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      sourcePostgresServerCASecretName,
	}
	err := env.Client.Get(env.Ctx, secretNamespacedName, secret)
	Expect(err).ToNot(HaveOccurred())
	caPair, err := certs.ParseCASecret(secret)
	Expect(err).ToNot(HaveOccurred())

	serverPair, err := caPair.CreateAndSignPair("pgbouncer", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())
	serverSecret := serverPair.GenerateCertificateSecret(namespace, secretName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

// createPoolerClientCertificateSecret creates a new CA secret and a new certificate
// secret for the pooler.
// The pooler uses this certificate to verify client connections to the pooler.
func createPoolerClientCertificateSecret(
	namespace, poolerName, caSecretName, tlsSecretName string, includeCAPrivateKey bool,
) {
	_, caPair, err := secrets.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, poolerName, caSecretName, includeCAPrivateKey)
	Expect(err).ToNot(HaveOccurred())

	altDNSNames := []string{
		"localhost", // Required to allow connecting via port-forwarding using "localhost" as the host
		poolerName,
		fmt.Sprintf("%v.%v", poolerName, namespace),
		fmt.Sprintf("%v.%v.svc", poolerName, namespace),
		fmt.Sprintf("%v.%v.svc.%s", poolerName, namespace, "cluster.local"),
	}
	clientPair, err := caPair.CreateAndSignPair(poolerName, certs.CertTypeServer, altDNSNames)
	Expect(err).ToNot(HaveOccurred())
	clientSecret := clientPair.GenerateCertificateSecret(namespace, tlsSecretName)
	err = env.Client.Create(env.Ctx, clientSecret)
	Expect(err).ToNot(HaveOccurred())
}

// createAppClientCertificates fetches an existing pooler client CA secret and creates
// certificates to allow connecting to the pooler from a client.
// It returns the paths to the client CA certificate, client certificate and client key.
func createAppClientCertificates(namespace, commonName, sourceCASecretName string,
) (caCertPath, clientCertPath, clientKeyPath string) {
	caSecret := &corev1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      sourceCASecretName,
	}
	err := env.Client.Get(env.Ctx, secretNamespacedName, caSecret)
	Expect(err).ToNot(HaveOccurred())
	caPair, err := certs.ParseCASecret(caSecret)
	Expect(err).ToNot(HaveOccurred())

	clientPair, err := caPair.CreateAndSignPair(commonName, certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	tmpDir := GinkgoT().TempDir()
	caCert := filepath.Join(tmpDir, "ca.crt")
	clientKey := filepath.Join(tmpDir, "client.key")
	clientCert := filepath.Join(tmpDir, "client.crt")
	err = os.WriteFile(caCert, caPair.Certificate, 0o600)
	Expect(err).ToNot(HaveOccurred())
	err = os.WriteFile(clientKey, clientPair.Private, 0o600)
	Expect(err).ToNot(HaveOccurred())
	err = os.WriteFile(clientCert, clientPair.Certificate, 0o600)
	Expect(err).ToNot(HaveOccurred())

	return caCert, clientCert, clientKey
}
