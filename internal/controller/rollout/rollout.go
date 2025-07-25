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

// Manager is the rollout manager. It is safe to use
// concurrently
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

	// The following data is relative to the last
	// rollout
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

// CoordinateRollout is called to check whether this rollout is allowed or not
// by the manager
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
