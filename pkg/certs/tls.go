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

package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type contextKey string

// contextKeyTLSConfig is the context key holding the TLS configuration
const contextKeyTLSConfig contextKey = "tlsConfig"

// newTLSConfigFromSecret creates a tls.Config from the given CA secret.
func newTLSConfigFromSecret(
	ctx context.Context,
	cli client.Client,
	caSecret types.NamespacedName,
) (*tls.Config, error) {
	secret := &corev1.Secret{}
	err := cli.Get(ctx, caSecret, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting caSecret %s: %w", caSecret.Name, err)
	}

	caCertificate, ok := secret.Data[CACertKey]
	if !ok {
		return nil, fmt.Errorf("missing %s entry in secret %s", CACertKey, caSecret.Name)
	}

	// The operator will verify the certificates only against the CA, ignoring the DNS name.
	// This behavior is because user-provided certificates could not have the DNS name
	// for the <cluster>-rw service, which would cause a name verification error.
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertificate)

	return NewTLSConfigFromCertPool(caCertPool), nil
}

// NewTLSConfigFromCertPool creates a tls.Config object from X509 cert pool
// containing the expected server CA
func NewTLSConfigFromCertPool(
	certPool *x509.CertPool,
) *tls.Config {
	tlsConfig := tls.Config{
		MinVersion:         tls.VersionTLS13,
		RootCAs:            certPool,
		InsecureSkipVerify: true, //#nosec G402 -- we are verifying the certificate ourselves
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			// Code adapted from https://go.dev/src/crypto/tls/handshake_client.go#L986
			if len(rawCerts) == 0 {
				return fmt.Errorf("no raw certificates provided")
			}

			certs := make([]*x509.Certificate, len(rawCerts))
			for i, rawCert := range rawCerts {
				cert, err := x509.ParseCertificate(rawCert)
				if err != nil {
					return fmt.Errorf("failed to parse certificate: %v", err)
				}
				certs[i] = cert
			}

			opts := x509.VerifyOptions{
				Roots:         certPool,
				Intermediates: x509.NewCertPool(),
			}

			for _, cert := range certs[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := certs[0].Verify(opts)
			if err != nil {
				return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
			}

			return nil
		},
	}

	return &tlsConfig
}

// NewTLSConfigForContext creates a tls.config with the provided data and returns an expanded context that contains
// the *tls.Config
func NewTLSConfigForContext(
	ctx context.Context,
	cli client.Client,
	caSecret types.NamespacedName,
) (context.Context, error) {
	conf, err := newTLSConfigFromSecret(ctx, cli, caSecret)
	if err != nil {
		return ctx, err
	}

	return context.WithValue(ctx, contextKeyTLSConfig, conf), nil
}

// GetTLSConfigFromContext returns the *tls.Config contained by the context or any error encountered
func GetTLSConfigFromContext(ctx context.Context) (*tls.Config, error) {
	conf, ok := ctx.Value(contextKeyTLSConfig).(*tls.Config)
	if !ok || conf == nil {
		return nil, fmt.Errorf("context does not contain TLSConfig")
	}
	return conf, nil
}
