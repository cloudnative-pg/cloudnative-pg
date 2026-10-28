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

package management

import (
	"context"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// shutdownLogExportTimeout bounds how long an unreachable OpenTelemetry
// collector can delay the termination of the process
const shutdownLogExportTimeout = 5 * time.Second

// ShutdownLogExport exports the log records still queued for the
// OpenTelemetry collector and stops the export, so that the later calls
// return at once. Call it before the process exits or replaces itself,
// while the network is still available.
func ShutdownLogExport() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownLogExportTimeout)
	defer cancel()

	if err := log.ShutdownOpenTelemetry(ctx); err != nil {
		log.Warning("Cannot export the queued log records via OpenTelemetry", "err", err)
	}
}
