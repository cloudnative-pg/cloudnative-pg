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

// Package secrets provides Ginkgo/Gomega assertions for credential
// secret rotation, TLS certificate provisioning, and TLS verification
// from application pods.
//
// Callers that also import tests/utils/secrets should alias one of the
// two to avoid the package name collision.
package secrets

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	secretsutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertUpdateSecret rotates the named field of the secret to the given
// value and waits until the cluster status reports the corresponding
// SecretsResourceVersion.
func AssertUpdateSecret(
	env *environment.TestingEnvironment,
	namespace, clusterName, secretName, field, value string,
	timeout int,
) {
	GinkgoHelper()
	var secret corev1.Secret

	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx,
			ctrlclient.ObjectKey{Namespace: namespace, Name: secretName},
			&secret)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	secret.Data[field] = []byte(value)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return env.Client.Update(env.Ctx, &secret)
	})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() string {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		if err != nil {
			GinkgoWriter.Printf("Error reports while retrieving cluster %v\n", err.Error())
			return ""
		}
		switch {
		case strings.HasSuffix(secretName, apiv1.ApplicationUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of %s secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.ApplicationSecretVersion)
			return cluster.Status.SecretsResourceVersion.ApplicationSecretVersion

		case strings.HasSuffix(secretName, apiv1.SuperUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of %s secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.SuperuserSecretVersion)
			return cluster.Status.SecretsResourceVersion.SuperuserSecretVersion

		case cluster.UsesSecretInManagedRoles(secretName):
			GinkgoWriter.Printf("Resource version of %s ManagedRole secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.ManagedRoleSecretVersions[secretName])
			return cluster.Status.SecretsResourceVersion.ManagedRoleSecretVersions[secretName]

		default:
			GinkgoWriter.Printf("Unsupported secrets name found %v\n", secretName)
			return ""
		}
	}, timeout).Should(BeEquivalentTo(secret.ResourceVersion))
}

// CreateAndAssertServerCertificatesSecrets provisions a self-signed CA
// secret plus a server-certificate secret signed by it, with the cluster
// DNS names plus "localhost" in SAN.
func CreateAndAssertServerCertificatesSecrets(
	env *environment.TestingEnvironment,
	namespace, clusterName, caSecName, tlsSecName string,
	includeCAPrivateKey bool,
) {
	GinkgoHelper()
	cluster, caPair, err := secretsutils.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, clusterName, caSecName, includeCAPrivateKey,
	)
	Expect(err).ToNot(HaveOccurred())

	altDNSNames := cluster.GetClusterAltDNSNames()
	// Required to allow connecting via port-forwarding using "localhost" as the host
	altDNSNames = append(altDNSNames, "localhost")

	serverPair, err := caPair.CreateAndSignPair(cluster.GetServiceReadWriteName(), certs.CertTypeServer, altDNSNames)
	Expect(err).ToNot(HaveOccurred())
	serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

// CreateAndAssertClientCertificatesSecrets provisions a self-signed CA
// plus two client certificates: one for the streaming_replica user and
// one for the application user.
func CreateAndAssertClientCertificatesSecrets(
	env *environment.TestingEnvironment,
	namespace, clusterName, caSecName, tlsSecName, userSecName string,
	includeCAPrivateKey bool,
) {
	GinkgoHelper()
	_, caPair, err := secretsutils.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, clusterName, caSecName, includeCAPrivateKey,
	)
	Expect(err).ToNot(HaveOccurred())

	// Sign tls certificates for streaming_replica user
	serverPair, err := caPair.CreateAndSignPair("streaming_replica", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())

	// Creating 'app' user tls certificates to validate connection from psql client
	serverPair, err = caPair.CreateAndSignPair("app", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	serverSecret = serverPair.GenerateCertificateSecret(namespace, userSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

// AssertSSLVerifyFullDBConnectionFromAppPod verifies that the app pod can
// reach the cluster's -rw service over TLS with sslmode=verify-full,
// presenting a client certificate.
func AssertSSLVerifyFullDBConnectionFromAppPod(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	appPod corev1.Pod,
) {
	GinkgoHelper()
	By("creating an app Pod and connecting to DB, using Certificate authentication", func() {
		Eventually(func() (string, string, error) {
			dsn := fmt.Sprintf("host=%v-rw.%v.svc port=5432 "+
				"sslkey=/etc/secrets/tls/tls.key "+
				"sslcert=/etc/secrets/tls/tls.crt "+
				"sslrootcert=/etc/secrets/ca/ca.crt "+
				"dbname=app user=app sslmode=verify-full", clusterName, namespace)
			timeout := time.Second * 10
			stdout, stderr, err := exec.Command(
				env.Ctx, env.Interface, env.RestClientConfig,
				appPod, appPod.Spec.Containers[0].Name, &timeout,
				"psql", dsn, "-tAc", "SELECT 1",
			)
			return stdout, stderr, err
		}, 360).Should(BeEquivalentTo("1\n"))
	})
}
