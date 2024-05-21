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

// NewTLSFromSecret creates a tls.Config from the given ca secret and serverName pair
func NewTLSFromSecret(
	ctx context.Context,
	c client.Client,
	caSecret types.NamespacedName,
	serverName string,
) (*tls.Config, error) {
	secret := &v1.Secret{}
	err := c.Get(ctx, caSecret, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret %s: %w", caSecret.Name, err)
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
