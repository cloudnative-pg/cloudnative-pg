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

package replication

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// placeholderInstanceNameSuffix is the name of the suffix to be added to the
// cluster name in order to create a fake instance name to be used in
// `synchronous_stanby_names` when the replica list would be empty.
const placeholderInstanceNameSuffix = "-placeholder"

func explicitSynchronousStandbyNames(cluster *apiv1.Cluster) string {
	config := cluster.Spec.PostgresConfiguration.Synchronous

	// Create the list of pod names
	clusterInstancesList := getSortedInstanceNames(cluster)
	if config.MaxStandbyNamesFromCluster != nil && len(clusterInstancesList) > *config.MaxStandbyNamesFromCluster {
		clusterInstancesList = clusterInstancesList[:*config.MaxStandbyNamesFromCluster]
	}

	// Add prefix and suffix
	instancesList := config.StandbyNamesPre
	instancesList = append(instancesList, clusterInstancesList...)
	instancesList = append(instancesList, config.StandbyNamesPost...)

	if len(instancesList) == 0 {
		instancesList = []string{
			cluster.Name + placeholderInstanceNameSuffix,
		}
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

// getSortedInstanceNames gets a list of all the known PostgreSQL instances in a
// order that would be meaningful to be used by `synchronous_standby_names`.
//
// The result is composed by:
//
//   - the list of non-primary ready instances - these are most likely the
//     instances to be used as a potential synchronous replicas
//   - the list of non-primary non-ready instances
//   - the name of the primary instance
//
// This algorithm have been designed to produce an order that would be
// meaningful to be used with priority-based synchronous replication (using the
// `first` method), while using the `maxStandbyNamesFromCluster` parameter.
func getSortedInstanceNames(cluster *apiv1.Cluster) []string {
	nonPrimaryReadyInstances := make([]string, 0, cluster.Spec.Instances)
	otherInstances := make([]string, 0, cluster.Spec.Instances)
	primaryInstance := ""

	for state, instanceList := range cluster.Status.InstancesStatus {
		for _, instance := range instanceList {
			switch {
			case cluster.Status.CurrentPrimary == instance:
				primaryInstance = instance

			case state == apiv1.PodHealthy:
				nonPrimaryReadyInstances = append(nonPrimaryReadyInstances, instance)
			}
		}
	}

	for _, instance := range cluster.Status.InstanceNames {
		if instance == primaryInstance {
			continue
		}

		if !slices.Contains(nonPrimaryReadyInstances, instance) {
			otherInstances = append(otherInstances, instance)
		}
	}

	sort.Strings(nonPrimaryReadyInstances)
	sort.Strings(otherInstances)
	result := make([]string, 0, cluster.Spec.Instances)
	result = append(result, nonPrimaryReadyInstances...)
	result = append(result, otherInstances...)
	if len(primaryInstance) > 0 {
		result = append(result, primaryInstance)
	}

	return result
}
