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

package external

import (
	"context"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dumpSecretKeyRefToFile", func() {
	const (
		namespace  = "test-namespace"
		serverName = "test-server"
		secretName = "test-secret"
		secretKey  = "sslrootcert"
	)

	var (
		ctx        context.Context
		fakeClient client.Client
		selector   *corev1.SecretKeySelector
	)

	buildClient := func(value []byte) client.Client {
		return fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					secretKey: value,
				},
			}).
			Build()
	}

	BeforeEach(func() {
		ctx = context.Background()
		// redirect the dump directory to an isolated temporary path
		customExternalSecretsPath = GinkgoT().TempDir()
		DeferCleanup(func() { customExternalSecretsPath = "" })

		selector = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  secretKey,
		}
	})

	It("writes the secret content to a file with 0600 permissions", func() {
		value := []byte("first-version")
		fakeClient = buildClient(value)

		path, err := dumpSecretKeyRefToFile(ctx, fakeClient, namespace, serverName, selector)
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(path) //nolint:gosec
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(Equal(value))

		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})

	It("fully overwrites the file when the secret rotates to shorter content", func() {
		// First reconciliation: a longer value (e.g. a two-certificate CA bundle)
		longValue := []byte(
			"-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n" +
				"-----BEGIN CERTIFICATE-----\nBBBB\n-----END CERTIFICATE-----\n")
		path, err := dumpSecretKeyRefToFile(
			ctx, buildClient(longValue), namespace, serverName, selector)
		Expect(err).ToNot(HaveOccurred())

		// Second reconciliation: the secret is rotated to a shorter value
		// (a single certificate) written to the very same path.
		shortValue := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")
		path2, err := dumpSecretKeyRefToFile(
			ctx, buildClient(shortValue), namespace, serverName, selector)
		Expect(err).ToNot(HaveOccurred())
		Expect(path2).To(Equal(path))

		// The file must contain exactly the new value, with no stale trailing
		// bytes left over from the previous, longer content.
		content, err := os.ReadFile(path2) //nolint:gosec
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(Equal(shortValue))
		Expect(content).ToNot(ContainSubstring("BBBB"))
	})
})

var _ = Describe("validateExternalClusterPaths", func() {
	sslSelector := func(name, key string) *corev1.SecretKeySelector {
		return &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
			Key:                  key,
		}
	}

	It("accepts a valid cluster with all SSL selectors set", func() {
		cluster := &apiv1.ExternalCluster{
			Name:        "my-server",
			SSLCert:     sslSelector("cert-secret", "tls.crt"),
			SSLKey:      sslSelector("cert-secret", "tls.key"),
			SSLRootCert: sslSelector("ca-secret", "ca.crt"),
			Password:    sslSelector("pass-secret", "password"),
		}
		Expect(validateExternalClusterPaths(cluster)).To(Succeed())
	})

	It("accepts a valid cluster with no SSL selectors set", func() {
		cluster := &apiv1.ExternalCluster{Name: "my-server"}
		Expect(validateExternalClusterPaths(cluster)).To(Succeed())
	})

	It("does not validate the password selector (not used as a path component)", func() {
		cluster := &apiv1.ExternalCluster{
			Name:     "my-server",
			Password: sslSelector("../pwned", "../pwned"),
		}
		Expect(validateExternalClusterPaths(cluster)).To(Succeed())
	})

	DescribeTable("rejects path components that would escape the secrets directory",
		func(cluster *apiv1.ExternalCluster) {
			Expect(validateExternalClusterPaths(cluster)).To(MatchError(ErrInvalidPathComponent))
		},
		Entry("traversal in server name",
			&apiv1.ExternalCluster{Name: "../pwned"}),
		Entry("separator in server name",
			&apiv1.ExternalCluster{Name: "nested/pwned"}),
		Entry("backslash in server name",
			&apiv1.ExternalCluster{Name: `nested\pwned`}),
		Entry("absolute path as server name",
			&apiv1.ExternalCluster{Name: "/etc/pwned"}),
		Entry("dot as server name",
			&apiv1.ExternalCluster{Name: "."}),
		Entry("parent-dir as server name",
			&apiv1.ExternalCluster{Name: ".."}),
		Entry("traversal in SSLCert secret name",
			&apiv1.ExternalCluster{Name: "ok", SSLCert: sslSelector("../pwned", "tls.crt")}),
		Entry("traversal in SSLCert key",
			&apiv1.ExternalCluster{Name: "ok", SSLCert: sslSelector("cert-secret", "../pwned")}),
		Entry("traversal in SSLKey secret name",
			&apiv1.ExternalCluster{Name: "ok", SSLKey: sslSelector("../pwned", "tls.key")}),
		Entry("traversal in SSLKey key",
			&apiv1.ExternalCluster{Name: "ok", SSLKey: sslSelector("cert-secret", "../pwned")}),
		Entry("traversal in SSLRootCert secret name",
			&apiv1.ExternalCluster{Name: "ok", SSLRootCert: sslSelector("../pwned", "ca.crt")}),
		Entry("traversal in SSLRootCert key",
			&apiv1.ExternalCluster{Name: "ok", SSLRootCert: sslSelector("ca-secret", "../pwned")}),
	)
})
