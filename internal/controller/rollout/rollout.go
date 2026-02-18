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

package rollout

import (
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The type of functions returning a moment in time
type timeFunc func() time.Time

// Manager is the rollout manager. It is safe to use concurrently.
//
// A single Manager instance is shared across all cluster reconciliations.
// It tracks the most recent rollout in a single slot (lastCluster,
// lastInstance, lastUpdate). This means only one rollout can be in
// progress at a time: any call to [Manager.CoordinateRollout] that
// arrives before the applicable delay has elapsed will be denied.
type Manager struct {
	m sync.Mutex

	// The amount of time we wait between rollouts of
	// different clusters
	clusterRolloutDelay time.Duration

	// The amount of time we wait between instances of
	// the same cluster
	instanceRolloutDelay time.Duration

	// This is used to get the current time. Mainly
	// used by the unit tests to inject a fake time
	timeProvider timeFunc

	// The following fields form the single global slot.
	// They record which cluster and instance last performed
	// a rollout, and when. All scheduling decisions are
	// derived from these three values.
	lastInstance string
	lastCluster  client.ObjectKey
	lastUpdate   time.Time
}

// Result is the output of the rollout manager, telling the
// operator how much time we need to wait to rollout a Pod
type Result struct {
	// This is true when the Pod can be rolled out immediately
	RolloutAllowed bool

	// This is set with the amount of time the operator need
	// to wait to rollout that Pod
	TimeToWait time.Duration
}

// New creates a new rollout manager with the passed configuration
func New(clusterRolloutDelay, instancesRolloutDelay time.Duration) *Manager {
	return &Manager{
		timeProvider:         time.Now,
		clusterRolloutDelay:  clusterRolloutDelay,
		instanceRolloutDelay: instancesRolloutDelay,
	}
}

// CoordinateRollout checks whether a Pod rollout is allowed and, when
// allowed, claims the global slot by recording the cluster, instance,
// and current time.
//
// Callers must only invoke this method when they intend to actually
// perform a rollout. Calling it without following through (e.g. for a
// supervised primary that only waits for user action) would occupy the
// slot and block other clusters from rolling out.
func (manager *Manager) CoordinateRollout(
	cluster client.ObjectKey,
	instanceName string,
) Result {
	manager.m.Lock()
	defer manager.m.Unlock()

	if manager.lastCluster == cluster {
		return manager.coordinateRolloutWithTime(cluster, instanceName, manager.instanceRolloutDelay)
	}
	return manager.coordinateRolloutWithTime(cluster, instanceName, manager.clusterRolloutDelay)
}

func (manager *Manager) coordinateRolloutWithTime(
	cluster client.ObjectKey,
	instanceName string,
	t time.Duration,
) Result {
	now := manager.timeProvider()
	timeSinceLastRollout := now.Sub(manager.lastUpdate)

	if manager.lastUpdate.IsZero() || timeSinceLastRollout >= t {
		manager.lastCluster = cluster
		manager.lastInstance = instanceName
		manager.lastUpdate = now
		return Result{
			RolloutAllowed: true,
			TimeToWait:     0,
		}
	}

	return Result{
		RolloutAllowed: false,
		TimeToWait:     t - timeSinceLastRollout,
	}
}
