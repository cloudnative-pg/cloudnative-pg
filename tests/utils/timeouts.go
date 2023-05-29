/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"encoding/json"
	"fmt"
	"os"
)

// TestTimeoutsEnvVar is the environment variable where specific timeouts can be
// set for the E2E test suite
const TestTimeoutsEnvVar = "TEST_TIMEOUTS"

// Timeout represents an event whose time we want to limit in the test suite
type Timeout string

// the events we're setting timeouts for
// NOTE: the text representation will be used as the fields in the JSON representation
// of the timeout object passed to the ginkgo command as an environment variable
const (
	Failover                  Timeout = "failover"
	NamespaceCreation         Timeout = "namespaceCreation"
	ClusterIsReady            Timeout = "clusterIsReady"
	ClusterIsReadyQuick       Timeout = "clusterIsReadyQuick"
	ClusterIsReadySlow        Timeout = "clusterIsReadySlow"
	NewPrimaryAfterSwitchover Timeout = "newPrimaryAfterSwitchover"
	NewPrimaryAfterFailover   Timeout = "newPrimaryAfterFailover"
	NewTargetOnFailover       Timeout = "newTargetOnFailover"
	OperatorIsReady           Timeout = "operatorIsReady"
	LargeObject               Timeout = "largeObject"
	WalsInMinio               Timeout = "walsInMinio"
	MinioInstallation         Timeout = "minioInstallation"
	BackupIsReady             Timeout = "backupIsReady"
	DrainNode                 Timeout = "drainNode"
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
	OperatorIsReady:           120,
	LargeObject:               300,
	WalsInMinio:               60,
	MinioInstallation:         300,
	BackupIsReady:             180,
	DrainNode:                 900,
}

// Timeouts returns the map of timeouts, where each event gets the timeout specificed
// in the `TEST_TIMEOUTS` environment variable, or if not specified, takes the default
// value
func Timeouts() (map[Timeout]int, error) {
	if timeoutsEnv, exists := os.LookupEnv(TestTimeoutsEnvVar); exists {
		var timeouts map[Timeout]int
		err := json.Unmarshal([]byte(timeoutsEnv), &timeouts)
		if err != nil {
			return map[Timeout]int{},
				fmt.Errorf("TEST_TIMEOUTS env variable is not valid: %v", err)
		}
		for k, def := range DefaultTestTimeouts {
			val, found := timeouts[k]
			if found {
				timeouts[k] = val
			} else {
				timeouts[k] = def
			}
		}
		return timeouts, nil
	}

	return DefaultTestTimeouts, nil
}
