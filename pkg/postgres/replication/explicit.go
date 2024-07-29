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

package replication

import (
	"fmt"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

func explicitSynchronousStandbyNames(cluster *apiv1.Cluster) string {
	config := cluster.Spec.PostgresConfiguration.Synchronous

	// Create the list of pod names
	clusterInstancesList := getSortedNonPrimaryInstanceNames(cluster)
	if config.MaxStandbyNamesFromCluster != nil && len(clusterInstancesList) > *config.MaxStandbyNamesFromCluster {
		clusterInstancesList = clusterInstancesList[:*config.MaxStandbyNamesFromCluster]
	}

	// Add prefix and suffix
	instancesList := config.StandbyNamesPre
	instancesList = append(instancesList, clusterInstancesList...)
	instancesList = append(instancesList, config.StandbyNamesPost...)
	if len(instancesList) == 0 {
		return ""
	}

	// Escape the pod list
	escapedReplicas := make([]string, len(instancesList))
	for idx, name := range instancesList {
		escapedReplicas[idx] = escapePostgresConfLiteral(name)
	}

	return fmt.Sprintf(
		"%s %v (%v)",
		config.Method.ToPostgreSQLConfigurationKeyword(),
		config.Number,
		strings.Join(escapedReplicas, ","))
}
