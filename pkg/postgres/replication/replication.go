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
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// GetExpectedSyncReplicasNumber computes the actual number of required synchronous replicas
func GetExpectedSyncReplicasNumber(cluster *apiv1.Cluster) int {
	if cluster.Spec.PostgresConfiguration.Synchronous != nil {
		return cluster.Spec.PostgresConfiguration.Synchronous.Number
	}

	syncReplicas, _ := getSyncReplicasData(cluster)
	return syncReplicas
}

// GetSynchronousStandbyNames gets the value to be applied
// to synchronous_standby_names
func GetSynchronousStandbyNames(cluster *apiv1.Cluster) string {
	if cluster.Spec.PostgresConfiguration.Synchronous != nil {
		return explicitSynchronousStandbyNames(cluster)
	}

	return legacySynchronousStandbyNames(cluster)
}
