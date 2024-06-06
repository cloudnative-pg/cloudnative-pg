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
	"crypto/x509"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type contextKey string

// contextKeyTLSConfig is the context key holding the TLS configuration
const contextKeyTLSConfig contextKey = "tlsConfig"

// newTLSConfigFromSecret creates a tls.Config from the given CA secret and serverName pair
func newTLSConfigFromSecret(
	ctx context.Context,
	cli client.Client,
	caSecret types.NamespacedName,
	serverName string,
) (*tls.Config, error) {
	secret := &v1.Secret{}
	err := cli.Get(ctx, caSecret, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting caSecret %s: %w", caSecret.Name, err)
	}

	caCertificate, ok := secret.Data[CACertKey]
	if !ok {
		return nil, fmt.Errorf("missing %s entry in secret %s", CACertKey, caSecret.Name)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertificate)
	tlsConfig := tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: serverName,
		RootCAs:    caCertPool,
	}

	return &tlsConfig, nil
}

// NewTLSConfigForContext creates a tls.config with the provided data and returns an expanded context that contains
// the *tls.Config
func NewTLSConfigForContext(
	ctx context.Context,
	cli client.Client,
	caSecret types.NamespacedName,
	serverName string,
) (context.Context, error) {
	conf, err := newTLSConfigFromSecret(ctx, cli, caSecret, serverName)
	if err != nil {
		return nil, err
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
