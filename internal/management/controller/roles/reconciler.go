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

package roles

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
	contextLogger.Debug("Updating managed roles information")

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return reconcile.Result{}, err
	}

	// get current passwords from spec/secrets
	latestPasswordResourceVersion, err := getPasswordSecretResourceVersion(
		ctx, c, cluster.Spec.Managed.Roles, cluster.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	contextLogger.Debug("getting the managed roles status")
	rolesInDB, err := List(ctx, db)
	if err != nil {
		return reconcile.Result{}, err
	}

	rolesByStatus := evaluateNextRoleActions(
		ctx,
		cluster.Spec.Managed,
		rolesInDB,
		cluster.Status.ManagedRolesStatus.PasswordStatus,
		latestPasswordResourceVersion,
	).convertToRolesByStatus()

	roleNamesByStatus := make(map[apiv1.RoleStatus][]string)
	for status, roles := range rolesByStatus {
		roleNamesByStatus[status] = getRoleNames(roles)
	}

	if len(rolesByStatus[apiv1.RoleStatusPendingReconciliation]) != 0 {
		// triggers reconciliation actions on DB, if this is a primary
		isPrimary, err := instance.IsPrimary()
		if err != nil {
			return reconcile.Result{}, err
		}
		if isPrimary {
			instance.TriggerRoleSynchronizer(cluster.Spec.Managed)
			contextLogger.Info("Triggered a managed role reconciliation")
		}
	}

	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.ManagedRolesStatus.ByStatus = roleNamesByStatus
	return reconcile.Result{}, c.Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster))
}
