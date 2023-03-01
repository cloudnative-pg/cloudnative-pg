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

package roles

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// Reconcile reconciles the managed roles in the primary instance
func Reconcile(
	ctx context.Context,
	instance *postgres.Instance,
	cluster *apiv1.Cluster,
	statusClient client.StatusClient,
) (reconcile.Result, error) {
	if cluster.Spec.Managed == nil ||
		len(cluster.Spec.Managed.Roles) == 0 {
		return reconcile.Result{}, nil
	}

	if cluster.Status.CurrentPrimary != instance.PodName {
		return reconcile.Result{}, nil
	}

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return reconcile.Result{}, err
	}
	_ = NewPostgresRoleManager(db)

	updatedCluster := cluster.DeepCopy()

	updatedCluster.Status.Phase = apiv1.PhaseHealthy
	updatedCluster.Status.PhaseReason = "Primary instance restarted in-place"
	return reconcile.Result{}, statusClient.Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster))
}
