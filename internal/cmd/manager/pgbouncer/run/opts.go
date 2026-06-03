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

package run

import (
	"crypto/tls"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
)

type runOptions struct {
	poolerNamespacedName types.NamespacedName
	metricsPortTLS       bool
}

// metricsTLSConfig returns a TLS configuration for the metrics server,
// or nil if TLS is disabled. The certificate is loaded from disk on every
// handshake so that rotations are picked up without a restart.
func (opts runOptions) metricsTLSConfig() *tls.Config {
	if !opts.metricsPortTLS {
		return nil
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		// GetCertificate is called on every TLS handshake. Loading the key pair
		// from disk each time is intentional: it allows the certificate to be
		// rotated (e.g. by cert-manager) and picked up automatically without
		// restarting the process.
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(config.ClientTLSCertPath, config.ClientTLSKeyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load server key pair: %w", err)
			}
			return &cert, nil
		},
	}
}
