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

package common

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

// NewHTTPClient returns a client capable of executing HTTP methods both in HTTPS and HTTP depending on the passed
// context
func NewHTTPClient(connectionTimeout, requestTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: connectionTimeout}

	return &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				tlsConfig, err := certs.GetTLSConfigFromContext(ctx)
				if err != nil {
					return nil, err
				}
				tlsDialer := tls.Dialer{
					NetDialer: dialer,
					Config:    tlsConfig,
				}
				return tlsDialer.DialContext(ctx, network, addr)
			},
		},
		Timeout: requestTimeout,
	}
}
