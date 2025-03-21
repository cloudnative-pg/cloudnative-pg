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

// Package connection represents a connected CNPG-i plugin
package connection

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/timeout"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	contextutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils/context"
)

// ProtocolUnix is for plugins that are reachable over a
// UNIX domain socket
type ProtocolUnix string

// Dial implements the Protocol interface
func (p ProtocolUnix) Dial(ctx context.Context) (Handler, error) {
	contextLogger := log.FromContext(ctx)
	dialPath := fmt.Sprintf("unix://%s", p)

	contextLogger.Debug("Connecting to plugin via local socket", "path", dialPath)

	timeoutValue := defaultTimeout
	value, ok := ctx.Value(contextutils.GRPCTimeoutKey).(time.Duration)
	if ok {
		contextLogger.Debug("Using custom timeout value", "timeout", value)
		timeoutValue = value
	}

	return grpc.NewClient(
		dialPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(timeout.UnaryClientInterceptor(timeoutValue)))
}
