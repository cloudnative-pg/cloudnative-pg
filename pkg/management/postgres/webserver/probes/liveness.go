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

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/watchdog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

type livenessExecutor struct {
	cache         *ClusterCache
	instance      *postgres.Instance
	leaseWatchdog *watchdog.LeaseWatchdog
}

// NewLivenessChecker creates a new instance of the liveness probe checker.
// leaseWatchdog reports whether the primary-lease loop (see
// internal/cmd/manager/instance/run/lease) is still attempting work; it does
// not fence on lease renewal failures alone, since a primary that cannot
// reach the API server but has confirmed via checkStepDown that it should
// stay primary must keep running.
func NewLivenessChecker(
	instance *postgres.Instance,
	cache *ClusterCache,
	leaseWatchdog *watchdog.LeaseWatchdog,
) Checker {
	return &livenessExecutor{
		cache:         cache,
		instance:      instance,
		leaseWatchdog: leaseWatchdog,
	}
}

// IsHealthy fails the liveness probe in the two cases the primary lease and
// fencing logic cannot recover from on their own:
//
//  1. The primary-lease loop has stopped attempting work altogether (a stuck
//     goroutine, e.g. a deadlock) - as opposed to merely failing to renew,
//     which is an expected, separately handled condition. leaseWatchdog only
//     flags this once this instance has actually acquired the lease at least
//     once, so replicas (and a primary still competing for the lease for the
//     first time) are never fenced by it. Besides failing the probe, this
//     also makes a best-effort attempt to shut PostgreSQL down immediately
//     itself: the stuck goroutine is typically the lease loop specifically,
//     not the whole process, so the lifecycle manager's command loop is
//     usually still able to act on this without waiting for the kubelet to
//     kill the container (which can take far longer than the primary lease
//     is willing to wait before a challenger takes over).
//  2. PostgreSQL was asked to shut down immediately (e.g. because this
//     primary had to step down) but did not honor it within its configured
//     timeout - e.g. a backend or the postmaster stuck in an uninterruptible
//     state, or stopped via SIGSTOP. The kubelet's forceful Pod termination
//     succeeds where the signal-based shutdown request did not.
func (e *livenessExecutor) IsHealthy(
	ctx context.Context,
	w http.ResponseWriter,
) {
	contextLogger := log.FromContext(ctx)

	if err := e.leaseWatchdog.IsHealthy(); err != nil {
		contextLogger.Warning("liveness probe failing: primary lease loop stuck", "err", err.Error())
		if e.instance.TryRequestImmediateShutdown() {
			contextLogger.Warning("requested immediate PostgreSQL shutdown due to stuck primary lease loop")
		}
		http.Error(w, fmt.Sprintf("primary lease loop stuck: %s", err), http.StatusInternalServerError)
		return
	}
	if e.instance.IsImmediateShutdownUnresponsive() {
		contextLogger.Warning("liveness probe failing: PostgreSQL unresponsive to immediate shutdown")
		http.Error(w, "PostgreSQL unresponsive to immediate shutdown", http.StatusInternalServerError)
		return
	}

	_, _ = fmt.Fprint(w, "OK")
}
