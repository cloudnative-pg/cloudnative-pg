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

// TLSConfigOptions holds the parameters required to build a TLS client configuration.
type TLSConfigOptions struct {
	// Client is the Kubernetes client used to fetch the CA secret.
	Client client.Client

	// CASecret is the namespaced name of the secret containing the server CA certificate.
	CASecret types.NamespacedName

	// ClientCert is the certificate presented to the server during the TLS handshake.
	// Pass nil when the caller does not need to authenticate itself (e.g. isolation
	// probes, diagnostic commands).
	ClientCert *tls.Certificate
}

// newTLSConfigFromSecret creates a tls.Config from the given CA secret.
func newTLSConfigFromSecret(ctx context.Context, opts TLSConfigOptions) (*tls.Config, error) {
	secret := &corev1.Secret{}
	err := opts.Client.Get(ctx, opts.CASecret, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting caSecret %s: %w", opts.CASecret.Name, err)
	}

	caCertificate, ok := secret.Data[CACertKey]
	if !ok {
		return nil, fmt.Errorf("missing %s entry in secret %s", CACertKey, opts.CASecret.Name)
	}

	// The operator will verify the certificates only against the CA, ignoring the DNS name.
	// This behavior is because user-provided certificates could not have the DNS name
	// for the <cluster>-rw service, which would cause a name verification error.
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertificate)

	return NewTLSConfigFromCertPool(caCertPool), nil
}

// verifyCertificates validates the peer certificate chain against the trusted CA pool.
func verifyCertificates(certPool *x509.CertPool, certs []*x509.Certificate) error {
	if len(certs) == 0 {
		return fmt.Errorf("no certificates provided")
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
		// VerifyConnection runs on every completed handshake, including resumed
		// TLS 1.3 sessions where no certificate exchange occurs but the original
		// peer certificates remain available in tls.ConnectionState.
		VerifyConnection: func(conn tls.ConnectionState) error {
			return verifyCertificates(certPool, conn.PeerCertificates)
		},
	}

	return &tlsConfig
}

// NewTLSConfigForContext creates a tls.Config from the given options and stores it
// in the returned context.
func NewTLSConfigForContext(ctx context.Context, opts TLSConfigOptions) (context.Context, error) {
	conf, err := newTLSConfigFromSecret(ctx, opts)
	if err != nil {
		return ctx, err
	}

	if opts.ClientCert != nil {
		conf.Certificates = []tls.Certificate{*opts.ClientCert}
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
