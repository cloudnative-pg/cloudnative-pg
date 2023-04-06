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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

type (
	rolesByAction map[roleAction][]apiv1.RoleConfiguration
	rolesByStatus map[apiv1.RoleStatus][]apiv1.RoleConfiguration
)

// convertToRolesByStatus gets the status of every role in the Spec and/or in the DB
func (r rolesByAction) convertToRolesByStatus() rolesByStatus {
	statusByAction := map[roleAction]apiv1.RoleStatus{
		roleCreate:            apiv1.RoleStatusPendingReconciliation,
		roleDelete:            apiv1.RoleStatusPendingReconciliation,
		roleUpdate:            apiv1.RoleStatusPendingReconciliation,
		roleSetComment:        apiv1.RoleStatusPendingReconciliation,
		roleUpdateMemberships: apiv1.RoleStatusPendingReconciliation,
		roleIsReconciled:      apiv1.RoleStatusReconciled,
		roleIgnore:            apiv1.RoleStatusNotManaged,
		roleIsReserved:        apiv1.RoleStatusReserved,
	}

	rolesByStatus := make(rolesByStatus)
	for action, roles := range r {
		// NOTE: several actions map to the PendingReconciliation status, so
		// we need to append the roles in each action
		rolesByStatus[statusByAction[action]] = append(rolesByStatus[statusByAction[action]], roles...)
	}

	return rolesByStatus
}

// evaluateNextRoleActions evaluates the action needed for each role in the DB and/or the Spec.
// It has no side effects
func evaluateNextRoleActions(
	ctx context.Context,
	config *apiv1.ManagedConfiguration,
	rolesInDB []DatabaseRole,
	lastPasswordState map[string]apiv1.PasswordState,
	latestSecretResourceVersion map[string]string,
) rolesByAction {
	contextLog := log.FromContext(ctx).WithName("roles_reconciler")
	contextLog.Debug("evaluating role actions")

	rolesInSpec := config.Roles
	// set up a map name -> role for the spec roles
	roleInSpecNamed := make(map[string]apiv1.RoleConfiguration)
	for _, r := range rolesInSpec {
		roleInSpecNamed[r.Name] = r
	}

	rolesByAction := make(rolesByAction)
	// 1. find the next actions for the roles in the DB
	roleInDBNamed := make(map[string]DatabaseRole)
	for _, role := range rolesInDB {
		roleInDBNamed[role.Name] = role
		inSpec, isInSpec := roleInSpecNamed[role.Name]
		switch {
		case postgres.IsRoleReserved(role.Name):
			rolesByAction[roleIsReserved] = append(rolesByAction[roleIsReserved], apiv1.RoleConfiguration{Name: role.Name})
		case isInSpec && inSpec.Ensure == apiv1.EnsureAbsent:
			rolesByAction[roleDelete] = append(rolesByAction[roleDelete], apiv1.RoleConfiguration{Name: role.Name})
		case isInSpec &&
			(!role.isEquivalentTo(inSpec) ||
				role.passwordNeedsUpdating(lastPasswordState, latestSecretResourceVersion)):
			rolesByAction[roleUpdate] = append(rolesByAction[roleUpdate], inSpec)
		case isInSpec && !role.hasSameCommentAs(inSpec):
			rolesByAction[roleSetComment] = append(rolesByAction[roleSetComment], inSpec)
		case isInSpec && !role.isInSameRolesAs(inSpec):
			rolesByAction[roleUpdateMemberships] = append(rolesByAction[roleUpdateMemberships], inSpec)
		case !isInSpec:
			rolesByAction[roleIgnore] = append(rolesByAction[roleIgnore], apiv1.RoleConfiguration{Name: role.Name})
		default:
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], apiv1.RoleConfiguration{Name: role.Name})
		}
	}

	// 2. get status of roles in spec missing from the DB
	for _, r := range rolesInSpec {
		_, isInDB := roleInDBNamed[r.Name]
		if isInDB {
			continue // covered by the previous loop
		}
		if r.Ensure == apiv1.EnsurePresent {
			rolesByAction[roleCreate] = append(rolesByAction[roleCreate], r)
		} else {
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], r)
		}
	}

	return rolesByAction
}
