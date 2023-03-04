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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// Reconcile triggers reconciliation of managed roles, gets their status, and
// updates it into the cluster Status
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

	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Updating managed roles information")

	// forces runnable to run
	instance.ConfigureRoleSynchronizer(cluster.Spec.Managed)
	contextLogger.Info("Updating managed roles information")

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return reconcile.Result{}, err
	}

	mgr := NewPostgresRoleManager(db)
	statusByRole, err := getRoleStatus(ctx, mgr, cluster.Spec.Managed)
	if err != nil {
		return reconcile.Result{}, err
	}

	// pivot the role status for display in the cluster Status
	rolesByStatus := make(map[apiv1.RoleStatus][]string)
	for role, status := range statusByRole {
		rolesByStatus[status] = append(rolesByStatus[status], role)
	}

	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.RoleStatus = rolesByStatus
	return reconcile.Result{}, statusClient.Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster))
}
