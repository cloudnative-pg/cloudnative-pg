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

package status

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// PatchWithOptimisticLock updates the status of the cluster using the passed
// transaction functions (in the given order).
// Important: after successfully updating the status, this
// function refreshes it into the passed cluster
func PatchWithOptimisticLock(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	txs ...Transaction,
) error {
	if cluster == nil {
		return nil
	}

	contextLogger := log.FromContext(ctx)

	origCluster := cluster.DeepCopy()

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var currentCluster apiv1.Cluster
		if err := c.Get(ctx, client.ObjectKeyFromObject(cluster), &currentCluster); err != nil {
			return err
		}

		updatedCluster := currentCluster.DeepCopy()
		for _, tx := range txs {
			tx(updatedCluster)
		}

		if equality.Semantic.DeepEqual(currentCluster.Status, updatedCluster.Status) {
			return nil
		}

		if err := c.Status().Patch(
			ctx,
			updatedCluster,
			client.MergeFromWithOptions(&currentCluster, client.MergeFromWithOptimisticLock{}),
		); err != nil {
			return err
		}

		cluster.Status = updatedCluster.Status

		return nil
	}); err != nil {
		return fmt.Errorf("while patching status: %w", err)
	}

	if cluster.Status.Phase != apiv1.PhaseHealthy && origCluster.Status.Phase == apiv1.PhaseHealthy {
		contextLogger.Info("Cluster has become unhealthy")
	}
	if cluster.Status.Phase == apiv1.PhaseHealthy && origCluster.Status.Phase != apiv1.PhaseHealthy {
		contextLogger.Info("Cluster has become healthy")
	}

	return nil
}
