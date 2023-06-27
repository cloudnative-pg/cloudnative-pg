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

package config

import (
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Secret type detection", func() {
	basicAuthData := map[string][]byte{
		corev1.BasicAuthUsernameKey: []byte("test-username"),
		corev1.BasicAuthPasswordKey: []byte("test-password"),
	}

	tlsAuthData := map[string][]byte{
		corev1.TLSCertKey:       []byte("tls.crt"),
		corev1.TLSPrivateKeyKey: []byte("tls.key"),
	}

	DescribeTable("Detecting the secret type",
		func(declaredType corev1.SecretType, data map[string][]byte, expectedType corev1.SecretType) {
			secret := &corev1.Secret{
				Type: declaredType,
				Data: data,
			}
			Expect(detectSecretType(secret)).To(Equal(expectedType))
		},
		Entry("for typed basic-auth secrets", corev1.SecretTypeBasicAuth, basicAuthData, corev1.SecretTypeBasicAuth),
		Entry("for typed tls secrets", corev1.SecretTypeTLS, tlsAuthData, corev1.SecretTypeTLS),
		Entry("for untyped basic-auth secrets", corev1.SecretTypeOpaque, basicAuthData, corev1.SecretTypeBasicAuth),
		Entry("for untyped tls secrets", corev1.SecretTypeOpaque, tlsAuthData, corev1.SecretTypeTLS),
	)

	It("fails when the type cannot be detected", func() {
		secretData := map[string][]byte{
			"test": []byte("toast"),
		}
		secret := &corev1.Secret{
			Type: corev1.SecretTypeOpaque,
			Data: secretData,
		}

		detectedType, err := detectSecretType(secret)
		Expect(detectedType).To(BeEmpty())
		Expect(err).ToNot(BeNil())
	})
})
