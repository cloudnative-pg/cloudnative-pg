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

package certs

import (
	"context"
	"crypto/tls"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("newTLSConfigFromSecret", func() {
	var (
		ctx      context.Context
		c        client.Client
		caSecret types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.TODO()
		caSecret = types.NamespacedName{Name: "test-secret", Namespace: "default"}
	})

	Context("when the secret is found and valid", func() {
		BeforeEach(func() {
			secretData := map[string][]byte{
				CACertKey: []byte(`-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA7Qe3X7Q6WZpXqlXkq0Bd
... (rest of the CA certificate) ...
-----END CERTIFICATE-----`),
			}
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecret.Name,
					Namespace: caSecret.Namespace,
				},
				Data: secretData,
			}
			c = fake.NewClientBuilder().WithObjects(secret).Build()
		})

		It("should return a valid tls.Config", func() {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(tlsConfig).NotTo(BeNil())
			Expect(tlsConfig.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
			Expect(tlsConfig.RootCAs).ToNot(BeNil())
		})
	})

	Context("when the secret is not found", func() {
		BeforeEach(func() {
			c = fake.NewClientBuilder().Build()
		})

		It("should return an error", func() {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).To(HaveOccurred())
			Expect(tlsConfig).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("while getting caSecret %s", caSecret.Name)))
		})
	})

	Context("when the ca.crt entry is missing in the secret", func() {
		BeforeEach(func() {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecret.Name,
					Namespace: caSecret.Namespace,
				},
			}
			c = fake.NewClientBuilder().WithObjects(secret).Build()
		})

		It("should return an error", func() {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).To(HaveOccurred())
			Expect(tlsConfig).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("missing %s entry in secret %s", CACertKey, caSecret.Name)))
		})
	})
})
