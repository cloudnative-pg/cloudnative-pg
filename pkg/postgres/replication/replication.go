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
	"context"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// GetExpectedSyncReplicasNumber computes the actual number of required synchronous replicas
func GetExpectedSyncReplicasNumber(ctx context.Context, cluster *apiv1.Cluster) int {
	if cluster.Spec.PostgresConfiguration.Synchronous != nil {
		return cluster.Spec.PostgresConfiguration.Synchronous.Number
	}

	syncReplicas, _ := getSyncReplicasData(ctx, cluster)
	return syncReplicas
}

// GetSynchronousStandbyNames gets the value to be applied
// to synchronous_standby_names
func GetSynchronousStandbyNames(ctx context.Context, cluster *apiv1.Cluster) postgres.SynchronousStandbyNamesConfig {
	if cluster.Spec.PostgresConfiguration.Synchronous != nil {
		return explicitSynchronousStandbyNames(cluster)
	}

	return legacySynchronousStandbyNames(ctx, cluster)
}
