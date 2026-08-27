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

// Package timeouts contains the timeouts for the E2E test suite
package timeouts

import (
	"fmt"
	"maps"

	"github.com/cloudnative-pg/cloudnative-pg/tests/config"
)

// Timeout represents an event whose time we want to limit in the test suite
type Timeout string

// the events we're setting timeouts for
// NOTE: the text representation will be used as the keys of the `timeouts`
// section of the e2e configuration file
const (
	Failover                  Timeout = "failover"
	NamespaceCreation         Timeout = "namespaceCreation"
	ClusterIsReady            Timeout = "clusterIsReady"
	ClusterIsReadyQuick       Timeout = "clusterIsReadyQuick"
	ClusterIsReadySlow        Timeout = "clusterIsReadySlow"
	NewPrimaryAfterSwitchover Timeout = "newPrimaryAfterSwitchover"
	NewPrimaryAfterFailover   Timeout = "newPrimaryAfterFailover"
	NewTargetOnFailover       Timeout = "newTargetOnFailover"
	PodRollout                Timeout = "podRollout"
	OperatorIsReady           Timeout = "operatorIsReady"
	LargeObject               Timeout = "largeObject"
	WalsInObjectStore         Timeout = "walsInObjectStore"
	ObjectStoreInstallation   Timeout = "objectStoreInstallation"
	BackupIsReady             Timeout = "backupIsReady"
	DrainNode                 Timeout = "drainNode"
	VolumeSnapshotIsReady     Timeout = "volumeSnapshotIsReady"
	Short                     Timeout = "short"
	ManagedServices           Timeout = "managedServices"
)

// DefaultTestTimeouts contains the default timeout in seconds for various events
var DefaultTestTimeouts = map[Timeout]int{
	Failover:                  240,
	NamespaceCreation:         30,
	ClusterIsReady:            600,
	ClusterIsReadyQuick:       300,
	ClusterIsReadySlow:        800,
	NewPrimaryAfterSwitchover: 45,
	NewPrimaryAfterFailover:   30,
	NewTargetOnFailover:       120,
	PodRollout:                180,
	OperatorIsReady:           120,
	LargeObject:               300,
	WalsInObjectStore:         60,
	ObjectStoreInstallation:   300,
	BackupIsReady:             180,
	DrainNode:                 900,
	VolumeSnapshotIsReady:     300,
	Short:                     5,
	ManagedServices:           30,
}

// Timeouts returns the map of timeouts, where each event gets the timeout
// specified in the `timeouts` section of the e2e configuration file, or if
// not specified, takes the default value
func Timeouts() (map[Timeout]int, error) {
	timeouts := make(map[Timeout]int, len(DefaultTestTimeouts))
	maps.Copy(timeouts, DefaultTestTimeouts)

	for k, val := range config.Current().Timeouts {
		if _, known := DefaultTestTimeouts[Timeout(k)]; !known {
			return nil, fmt.Errorf("unknown timeout %q in the e2e configuration", k)
		}
		timeouts[Timeout(k)] = val
	}

	return timeouts, nil
}
