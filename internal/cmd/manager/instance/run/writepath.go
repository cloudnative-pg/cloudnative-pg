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
	"context"

	"k8s.io/utils/clock"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/writepath"
)

// newWritePathMonitor creates the runnable executing the write path check of the
// primary instance. When the write path is stalled, the instance stops renewing
// its primary lease, so that a healthy replica can be promoted. See the
// `.spec.probes.liveness.writePathCheck` documentation.
func newWritePathMonitor(
	instance *postgres.Instance,
	leaseRunnable controller.PrimaryLeaseAcquirer,
) *writepath.Monitor {
	return &writepath.Monitor{
		State: &instance.WritePath,
		Clock: clock.RealClock{},
		Config: func() writepath.Config {
			cluster := instance.GetClusterOrDefault()
			return writepath.Config{
				Enabled:             cluster.IsWritePathCheckEnabled(),
				Period:              cluster.GetWritePathCheckPeriod(),
				Timeout:             cluster.GetWritePathCheckTimeout(),
				ClearAfterSuccesses: cluster.GetWritePathCheckClearAfterSuccesses(),
			}
		},
		ShouldRun: func() bool {
			if instance.MightBeUnavailable() || instance.IsFenced() {
				return false
			}
			isPrimary, err := instance.IsPrimary()
			if err != nil || !isPrimary {
				return false
			}
			return instance.IsReady() == nil
		},
		Beat: instance.WritePathHeartbeat,
		FailoverStarted: func() bool {
			targetPrimary := instance.GetClusterOrDefault().Status.TargetPrimary
			return targetPrimary != "" && targetPrimary != instance.GetPodName()
		},
		OnStalled: func() {
			// The primary can't serve writes: stop renewing the primary lease, so
			// that a healthy replica can take it over. The lease-age monitor keeps
			// running, so this primary is still fenced when it is superseded. The
			// instance reconciler takes the lease back through the regular
			// Acquire once the stall is over and this is still the target primary.
			leaseRunnable.StopRenewing(context.Background())
		},
	}
}
