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
	c client.Client,
) (reconcile.Result, error) {
	if cluster.Spec.Managed == nil ||
		len(cluster.Spec.Managed.Roles) == 0 {
		return reconcile.Result{}, nil
	}

	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Updating managed roles information")

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return reconcile.Result{}, err
	}

	mgr := NewPostgresRoleManager(db)
	// get current passwords from spec/secrets
	passwordsInSpec, err := getPasswordHashes(ctx, c, cluster.Spec.Managed.Roles, instance.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	latestPasswords := cluster.Status.RolePasswordStatus
	rolesByStatus, err := getRoleStatus(ctx, mgr, cluster.Spec.Managed, latestPasswords, passwordsInSpec)
	if err != nil {
		return reconcile.Result{}, err
	}
	roleNamesByStatus := make(map[apiv1.RoleStatus][]string)
	for status, roles := range rolesByStatus {
		roleNamesByStatus[status] = getRoleNames(roles)
	}

	if len(rolesByStatus[apiv1.RoleStatusPendingReconciliation]) != 0 {
		// forces runnable to run
		instance.TriggerRoleSynchronizer(cluster.Spec.Managed)
		contextLogger.Info("Triggered a managed role reconciliation")
	}

	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.RoleStatus = roleNamesByStatus
	return reconcile.Result{}, c.Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster))
}
