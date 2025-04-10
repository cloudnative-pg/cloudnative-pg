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

package controller

import (
	"database/sql"

	"github.com/jackc/pgx/v5/pgtype"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/roles"
)

// roleAdapter is an intermediary structure used to adapt a apiv1.RoleSpec
// to a roles.DatabaseRole.
type roleAdapter struct {
	apiv1.RoleSpec
	// validUntilNullIsInfinity indicates a null `validUntil` on the RoleConfiguration
	// should be translated in VALID UNTIL 'infinity' in the database.
	// This is needed because in Postgres you cannot restore a NULL value in the VALID UNTIL
	// field once you changed it.
	validUntilNullIsInfinity bool
}

// toDatabaseRole converts the contained apiv1.RoleConfiguration into the equivalent DatabaseRole
//
// NOTE: for passwords, the default behavior, if the RoleConfiguration does not either
// provide a PasswordSecret or explicitly set DisablePassword, is to IGNORE the password
func (role roleAdapter) toDatabaseRole() roles.DatabaseRole {
	dbRole := roles.DatabaseRole{
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
		dbRole.IgnorePassword = true
	case role.PasswordSecret == nil && role.DisablePassword:
		dbRole.Password = sql.NullString{}
	}
	return dbRole
}
