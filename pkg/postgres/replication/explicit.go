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
	"slices"
	"sort"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// placeholderInstanceNameSuffix is the name of the suffix to be added to the
// cluster name in order to create a fake instance name to be used in
// `synchronous_stanby_names` when the replica list would be empty.
const placeholderInstanceNameSuffix = "-placeholder"

func explicitSynchronousStandbyNames(cluster *apiv1.Cluster) postgres.SynchronousStandbyNamesConfig {
	switch cluster.Spec.PostgresConfiguration.Synchronous.DataDurability {
	case apiv1.DataDurabilityLevelPreferred:
		return explicitSynchronousStandbyNamesDataDurabilityPreferred(cluster)

	default:
		return explicitSynchronousStandbyNamesDataDurabilityRequired(cluster)
	}
}

func explicitSynchronousStandbyNamesDataDurabilityRequired(
	cluster *apiv1.Cluster,
) postgres.SynchronousStandbyNamesConfig {
	config := cluster.Spec.PostgresConfiguration.Synchronous

	// Create the list of pod names
	clusterInstancesList := getSortedInstanceNames(cluster)

	// Cap the number of standby names using the configuration on the cluster
	if config.MaxStandbyNamesFromCluster != nil && len(clusterInstancesList) > *config.MaxStandbyNamesFromCluster {
		clusterInstancesList = clusterInstancesList[:*config.MaxStandbyNamesFromCluster]
	}

	// Add prefix and suffix
	instancesList := make([]string, 0,
		len(clusterInstancesList)+len(config.StandbyNamesPre)+len(config.StandbyNamesPost))
	instancesList = append(instancesList, config.StandbyNamesPre...)
	instancesList = append(instancesList, clusterInstancesList...)
	instancesList = append(instancesList, config.StandbyNamesPost...)

	// An empty instances list would generate a PostgreSQL syntax error
	// because configuring synchronous replication with an empty replica
	// list is not allowed.
	// Adding this as a safeguard, but this should never get into a postgres configuration.
	if len(instancesList) == 0 {
		instancesList = []string{
			cluster.Name + placeholderInstanceNameSuffix,
		}
	}

	return postgres.SynchronousStandbyNamesConfig{
		Method:       config.Method.ToPostgreSQLConfigurationKeyword(),
		NumSync:      config.Number,
		StandbyNames: instancesList,
	}
}

func explicitSynchronousStandbyNamesDataDurabilityPreferred(
	cluster *apiv1.Cluster,
) postgres.SynchronousStandbyNamesConfig {
	config := cluster.Spec.PostgresConfiguration.Synchronous

	// Create the list of healthy replicas
	instancesList := getSortedNonPrimaryHealthyInstanceNames(cluster)

	// Cap the number of standby names using the configuration on the cluster
	if config.MaxStandbyNamesFromCluster != nil && len(instancesList) > *config.MaxStandbyNamesFromCluster {
		instancesList = instancesList[:*config.MaxStandbyNamesFromCluster]
	}

	// If data durability is not enforced, we cap the number of synchronous
	// replicas to be required to the number or available replicas.
	syncReplicaNumber := config.Number
	if syncReplicaNumber > len(instancesList) {
		syncReplicaNumber = len(instancesList)
	}

	// An empty instances list is not allowed in synchronous_standby_names
	if len(instancesList) == 0 {
		return postgres.SynchronousStandbyNamesConfig{
			Method:       "",
			NumSync:      0,
			StandbyNames: []string{},
		}
	}

	return postgres.SynchronousStandbyNamesConfig{
		Method:       config.Method.ToPostgreSQLConfigurationKeyword(),
		NumSync:      syncReplicaNumber,
		StandbyNames: instancesList,
	}
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
