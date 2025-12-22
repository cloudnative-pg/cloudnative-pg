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
	"database/sql"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5/pgtype"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

type (
	rolesByAction map[roleAction][]roleConfigurationAdapter
	rolesByStatus map[apiv1.RoleStatus][]roleConfigurationAdapter
)

// roleConfigurationAdapter is an intermediary structure used to adapt a apiv1.RoleConfiguration
// to a DatabaseRole.
type roleConfigurationAdapter struct {
	apiv1.RoleConfiguration
	// validUntilNullIsInfinity indicates a null `validUntil` on the RoleConfiguration
	// should be translated in VALID UNTIL 'infinity' in the database.
	// This is needed because in Postgres you cannot restore a NULL value in the VALID UNTIL
	// field once you changed it.
	validUntilNullIsInfinity bool
}

// roleAdapterFromName creates a roleConfigurationAdapter that only has the Name field
// populated. It is useful for operations such as DELETE or IGNORE that only need the name
func roleAdapterFromName(name string) roleConfigurationAdapter {
	return roleConfigurationAdapter{RoleConfiguration: apiv1.RoleConfiguration{Name: name}}
}

// toDatabaseRole converts the contained apiv1.RoleConfiguration into the equivalent DatabaseRole
//
// NOTE: for passwords, the default behavior, if the RoleConfiguration does not either
// provide a PasswordSecret or explicitly set DisablePassword, is to IGNORE the password
func (role roleConfigurationAdapter) toDatabaseRole() DatabaseRole {
	dbRole := DatabaseRole{
		Name:            role.Name,
		Comment:         role.Comment,
		Superuser:       role.Superuser,
		CreateDB:        role.CreateDB,
		CreateRole:      role.CreateRole,
		Inherit:         role.GetRoleInherit(),
		Login:           role.Login,
		Replication:     role.Replication,
		BypassRLS:       role.BypassRLS,
		ConnectionLimit: role.ConnectionLimit,
		InRoles:         role.InRoles,
	}
	switch {
	case role.ValidUntil != nil:
		dbRole.ValidUntil = pgtype.Timestamp{
			Valid: true,
			Time:  role.ValidUntil.Time,
		}
	case role.ValidUntil == nil && role.validUntilNullIsInfinity:
		dbRole.ValidUntil = pgtype.Timestamp{
			Valid:            true,
			InfinityModifier: pgtype.Infinity,
		}
	}
	switch {
	case role.PasswordSecret == nil && !role.DisablePassword:
		dbRole.ignorePassword = true
	case role.PasswordSecret == nil && role.DisablePassword:
		dbRole.password = sql.NullString{}
	}
	return dbRole
}

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
			rolesByAction[roleIsReserved] = append(rolesByAction[roleIsReserved],
				roleAdapterFromName(role.Name))
		case isInSpec && inSpec.Ensure == apiv1.EnsureAbsent:
			rolesByAction[roleDelete] = append(rolesByAction[roleDelete],
				roleAdapterFromName(role.Name))
		case isInSpec &&
			(!role.isEquivalentTo(inSpec) ||
				role.passwordNeedsUpdating(lastPasswordState, latestSecretResourceVersion)):
			internalRole := roleConfigurationAdapter{
				RoleConfiguration:        inSpec,
				validUntilNullIsInfinity: role.ValidUntil.Valid,
			}
			rolesByAction[roleUpdate] = append(rolesByAction[roleUpdate], internalRole)
		case isInSpec && !role.hasSameCommentAs(inSpec):
			internalRole := roleConfigurationAdapter{
				RoleConfiguration: inSpec,
			}
			rolesByAction[roleSetComment] = append(rolesByAction[roleSetComment], internalRole)
		case isInSpec && !role.isInSameRolesAs(inSpec):
			internalRole := roleConfigurationAdapter{
				RoleConfiguration: inSpec,
			}
			rolesByAction[roleUpdateMemberships] = append(rolesByAction[roleUpdateMemberships], internalRole)
		case !isInSpec:
			rolesByAction[roleIgnore] = append(rolesByAction[roleIgnore],
				roleAdapterFromName(role.Name))
		default:
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled],
				roleAdapterFromName(role.Name))
		}
	}

	// 2. get status of roles in spec missing from the DB
	for _, r := range rolesInSpec {
		_, isInDB := roleInDBNamed[r.Name]
		if isInDB {
			continue // covered by the previous loop
		}
		if r.Ensure == apiv1.EnsurePresent {
			internalRole := roleConfigurationAdapter{
				RoleConfiguration: r,
			}
			rolesByAction[roleCreate] = append(rolesByAction[roleCreate], internalRole)
		} else {
			rolesByAction[roleIsReconciled] = append(rolesByAction[roleIsReconciled], roleAdapterFromName(r.Name))
		}
	}

	return rolesByAction
}
