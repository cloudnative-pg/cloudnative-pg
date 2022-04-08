/*
Copyright 2019-2022 The CloudNativePG Contributors

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

// Package config contains the code related to the generation of the PgBouncer configuration
package config

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ErrorUnknownSecretType is raised when the detection of
// a Secret type failed
type ErrorUnknownSecretType struct {
	DeclaredType corev1.SecretType
	Keys         []string
}

// Error implements the error interface
func (e *ErrorUnknownSecretType) Error() string {
	return fmt.Sprintf("secret type or content is not accepted (type:%s keys:%s)",
		e.DeclaredType,
		strings.Join(e.Keys, ","))
}

// NewErrorUnknownSecretType returns a new error
// structure from the content of a secret
func NewErrorUnknownSecretType(secret *corev1.Secret) *ErrorUnknownSecretType {
	keys := make([]string, 0, len(secret.Data))
	for key := range secret.Data {
		keys = append(keys, key)
	}

	return &ErrorUnknownSecretType{
		DeclaredType: secret.Type,
		Keys:         keys,
	}
}

// detectSecretType finds the type of secret given the secret type itself
// or, if the secret have type Opaque, the list of keys
func detectSecretType(secret *corev1.Secret) (corev1.SecretType, error) {
	// When the user defined the secret type on its own, let's respect
	// this decision
	if secret.Type == corev1.SecretTypeTLS {
		return corev1.SecretTypeTLS, nil
	}

	if secret.Type == corev1.SecretTypeBasicAuth {
		return corev1.SecretTypeBasicAuth, nil
	}

	// We proceed on the detection given the keys that this secret
	// contains, but we avoid doing that is the user has chosen
	// a precise type that we don't know about
	if secret.Type != corev1.SecretTypeOpaque {
		return "", NewErrorUnknownSecretType(secret)
	}

	// If we have the username and the password, we assume that
	// this secret is of type BasicAuth
	_, containsUsername := secret.Data[corev1.BasicAuthUsernameKey]
	_, containsPassword := secret.Data[corev1.BasicAuthPasswordKey]

	if containsUsername && containsPassword {
		return corev1.SecretTypeBasicAuth, nil
	}

	// If we have the entries relative to the certificates,
	// we assume that this secret is of type TLS
	_, containsCertificate := secret.Data[corev1.TLSCertKey]
	_, containsPrivateKey := secret.Data[corev1.TLSPrivateKeyKey]

	if containsCertificate && containsPrivateKey {
		return corev1.SecretTypeTLS, nil
	}

	// Unfortunately we really don't know of what type
	// this secret is.
	return "", NewErrorUnknownSecretType(secret)
}
