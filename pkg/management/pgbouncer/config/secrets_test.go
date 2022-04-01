/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
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
