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

package probes

import (
	"context"
	"fmt"
	"net/http"
)

// livenessExecutor has no state left: the isolation fencing that used to need
// the cluster cache and the instance now lives in the primary lease. It is kept
// as the seam the liveness endpoint is wired through, so reintroducing a real
// check does not mean re-plumbing the web server.
type livenessExecutor struct{}

// NewLivenessChecker creates a new instance of the liveness probe checker
func NewLivenessChecker() Checker {
	return &livenessExecutor{}
}

// IsHealthy always reports success. Primary isolation fencing is handled by
// the primary lease (internal/cmd/manager/instance/run/lease) instead of the
// liveness probe.
func (e *livenessExecutor) IsHealthy(
	_ context.Context,
	w http.ResponseWriter,
) {
	_, _ = fmt.Fprint(w, "OK")
}
