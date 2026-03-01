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

package connection

import (
	"context"
	"crypto/tls"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/timeout"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// ProtocolTCP is the implementation of the Protocol interface
// for plugins that are reachable via an mTLS TCP connection
type ProtocolTCP struct {
	TLSConfig *tls.Config
	Address   string
}

// Dial implements the protocol interface
func (p *ProtocolTCP) Dial(_ context.Context) (Handler, error) {
	return grpc.NewClient(
		p.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(p.TLSConfig)),
		grpc.WithUnaryInterceptor(
			timeout.UnaryClientInterceptor(defaultNetworkCallTimeout),
		),
	)
}
