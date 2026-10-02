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

package controller

import (
	"context"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// systemIDNotFoundReason is the reason of the ConsistentSystemID condition
	// when no instance reported a system ID
	systemIDNotFoundReason = "NotFound"

	// systemIDRefreshDelay is how long to wait before sampling the instances
	// again when none of them reported a system ID
	systemIDRefreshDelay = 5 * time.Second
)

// withSystemIDRefresh requests a requeue on the result of a successful
// reconciliation that would not requeue, when the system ID has to be
// refreshed. It only covers the end of the loop: the earlier exits that stop
// it mostly requeue on their own.
func withSystemIDRefresh(
	ctx context.Context,
	result ctrl.Result,
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
) ctrl.Result {
	if result.IsZero() && shouldRefreshSystemID(ctx, cluster, statuses) {
		result.RequeueAfter = systemIDRefreshDelay
	}

	return result
}

// shouldRefreshSystemID returns true when instances were sampled but none
// reported a system ID, and none is fenced on purpose. That is a transient
// state, e.g. right after a demotion unfences them, that no event is
// guaranteed to clear. It only applies to replica clusters.
func shouldRefreshSystemID(
	ctx context.Context,
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
) bool {
	if !cluster.IsReplica() || len(statuses.Items) == 0 {
		return false
	}

	condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionConsistentSystemID))
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != systemIDNotFoundReason {
		return false
	}

	fencedInstances, err := utils.GetFencedInstances(cluster.Annotations)
	if err != nil {
		log.FromContext(ctx).Warning(
			"Cannot parse the fenced instances annotation, skipping the system ID refresh",
			"error", err)
		return false
	}

	return fencedInstances.Len() == 0
}
