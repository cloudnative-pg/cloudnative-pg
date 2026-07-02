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

// Package rollout contains the rollout manager, allowing
// CloudNativePG to spread Pod rollouts depending on
// the passed configuration.
//
// The manager uses a single global slot to enforce that only one Pod
// rollout proceeds at a time across all clusters managed by the
// operator. When [Manager.CoordinateRollout] is called and the rollout
// is allowed, the manager records the cluster, instance, and current
// timestamp. Subsequent calls are denied until the configured delay
// has elapsed: [Manager.clusterRolloutDelay] when the caller is a
// different cluster, or [Manager.instanceRolloutDelay] when it is the
// same cluster.
//
// Because the slot is global, callers that will not actually perform a
// rollout must avoid calling [Manager.CoordinateRollout]. For example,
// a cluster whose primary update strategy is "supervised" waits
// indefinitely for a user-initiated switchover; if it claimed the slot,
// it would block every other cluster from rolling out until the delay
// expires, and would re-claim it on every reconciliation loop,
// effectively starving other clusters.
//
// The delays are configured via the CLUSTERS_ROLLOUT_DELAY and
// INSTANCES_ROLLOUT_DELAY operator environment variables (both
// default to 0, meaning no delay).
package rollout
