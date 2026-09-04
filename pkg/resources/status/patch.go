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

// PatchObjectWithOptimisticLock applies the given transactions, in order, to a
// freshly read copy of the object and patches its status under an optimistic
// lock, retrying on conflict so a write landing between the read and the patch
// is never lost, even where a merge patch replaces a whole field (as it does
// for `status.conditions`). The object the caller holds is left alone; a zero
// value is returned when the patch was a no-op.
func PatchObjectWithOptimisticLock[T client.Object](
	ctx context.Context,
	c client.Client,
	obj T,
	txs ...func(T),
) (T, error) {
	var updated T
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current, ok := obj.DeepCopyObject().(T)
		if !ok {
			return fmt.Errorf("while copying %T to read its status", obj)
		}
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
			return err
		}

		next, ok := current.DeepCopyObject().(T)
		if !ok {
			return fmt.Errorf("while copying %T to patch its status", obj)
		}
		for _, tx := range txs {
			tx(next)
		}
		if equality.Semantic.DeepEqual(current, next) {
			return nil
		}

		if err := c.Status().Patch(ctx, next,
			client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}

		updated = next
		return nil
	})
	return updated, err
}

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

	updatedCluster, err := PatchObjectWithOptimisticLock(ctx, c, cluster, txs...)
	if err != nil {
		return fmt.Errorf("while patching status: %w", err)
	}
	if updatedCluster != nil {
		cluster.Status = updatedCluster.Status
	}

	if cluster.Status.Phase != apiv1.PhaseHealthy && origCluster.Status.Phase == apiv1.PhaseHealthy {
		contextLogger.Info("Cluster has become unhealthy")
	}
	if cluster.Status.Phase == apiv1.PhaseHealthy && origCluster.Status.Phase != apiv1.PhaseHealthy {
		contextLogger.Info("Cluster has become healthy")
	}

	return nil
}
