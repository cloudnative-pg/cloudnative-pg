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
	"database/sql"
	"fmt"
	"strings"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// PostgresRoleManager is a RoleManager for a database instance
type PostgresRoleManager struct {
	superUserDB *sql.DB
}

// NewPostgresRoleManager returns an implementation of RoleManager for postgres
func NewPostgresRoleManager(superDB *sql.DB) RoleManager {
	return PostgresRoleManager{
		superUserDB: superDB,
	}
}

// List the available roles
func (sm PostgresRoleManager) List(
	ctx context.Context,
	config *v1.ManagedConfiguration,
) ([]v1.RoleConfiguration, error) {
	rows, err := sm.superUserDB.QueryContext(
		ctx,
		`SELECT rolname, rolcreatedb, rolsuper, rolcanlogin, rolbypassrls,
		  rolpassword, rolvaliduntil
		FROM pg_catalog.pg_roles where rolname not like 'pg_%';`)
	// TODO: read rolinherit, rolcreaterole, rolreplication, rolconnlimit
	if err != nil {
		return []v1.RoleConfiguration{}, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var roles []v1.RoleConfiguration
	for rows.Next() {
		var validuntil, password sql.NullString
		var role v1.RoleConfiguration
		err := rows.Scan(
			&role.Name,
			&role.CreateDB,
			&role.Superuser,
			&role.Login,
			&role.BypassRLS,
			&password,
			&validuntil,
		)
		if err != nil {
			return []v1.RoleConfiguration{}, err
		}
		if validuntil.Valid {
			role.ValidUntil = validuntil.String
		}
		// TODO: should we check that the password is the same as the password
		// stored in the secret?

		roles = append(roles, role)
	}

	if rows.Err() != nil {
		return []v1.RoleConfiguration{}, rows.Err()
	}

	return roles, nil
}

// Update the role
func (sm PostgresRoleManager) Update(ctx context.Context, role v1.RoleConfiguration) error {
	contextLog := log.FromContext(ctx).WithName("updateRole")
	contextLog.Trace("Invoked", "role", role)

	var query strings.Builder
	query.WriteString("ALTER ROLE ")
	query.WriteString(role.Name)
	query.WriteString(" ")

	if role.BypassRLS {
		query.WriteString("BYPASSRLS ")
	} else {
		query.WriteString("NOBYPASSRLS ")
	}

	if role.CreateDB {
		query.WriteString("CREATEDB ")
	} else {
		query.WriteString("NOCREATEDB ")
	}

	if role.Login {
		query.WriteString("LOGIN ")
	} else {
		query.WriteString("NOLOGIN ")
	}

	if role.Superuser {
		query.WriteString("SUPERUSER ")
	} else {
		query.WriteString("NOSUPERUSER ")
	}

	contextLog.Info("Updating", "query", query.String())

	result, err := sm.superUserDB.ExecContext(ctx, query.String())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("could not update role %s", role.Name)
	}

	return nil
}

// Create the role
// TODO: do we give the role any database-level permissions?
func (sm PostgresRoleManager) Create(ctx context.Context, role v1.RoleConfiguration) (err error) {
	contextLog := log.FromContext(ctx).WithName("createRole")
	contextLog.Trace("Invoked", "role", role)

	// NOTE: defensively we might think of doint CREATE ... IF EXISTS
	// but at least during development, we want to catch the error
	// Even after, this may be "the kubernetes way"
	result, err := sm.superUserDB.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s", role.Name))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("could not create role %s, %d row updated", role.Name, rows)
	}

	return nil
}

// Delete the role
// TODO: we need to do something better here. We should not delete a user that
// has created tables or other objects
func (sm PostgresRoleManager) Delete(ctx context.Context, role v1.RoleConfiguration) error {
	contextLog := log.FromContext(ctx).WithName("dropRole")
	contextLog.Trace("Invoked", "role", role)

	result, err := sm.superUserDB.ExecContext(ctx, fmt.Sprintf("DROP ROLE %s", role.Name))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("could not delete role %s, %d row deleted", role.Name, rows)
	}

	return nil
}
