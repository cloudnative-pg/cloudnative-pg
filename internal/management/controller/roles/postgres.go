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
		`SELECT usename, usecreatedb, usesuper, userepl, usebypassrls, passwd, valuntil
		FROM pg_catalog.pg_user`)
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
	return nil
}

// Create the role
func (sm PostgresRoleManager) Create(ctx context.Context, role v1.RoleConfiguration) (err error) {
	contextLog := log.FromContext(ctx).WithName("createRole")
	contextLog.Trace("Invoked", "role", role)

	var tx *sql.Tx
	tx, err = sm.superUserDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("while creating a transaction: %w", err)
	}

	defer func() {
		if err == nil {
			err = tx.Commit()
			if err != nil {
				contextLog.Error(err, "could not commit cleanly", "role", role)
			}
			return
		}
		errRollback := tx.Rollback()
		if errRollback != nil {
			contextLog.Error(errRollback, "could not rollback cleanly", "role", role)
		}
	}()

	_, err = tx.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s", role.Name))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", "app", role.Name))
	if err != nil {
		return err
	}

	return nil
}

// Delete the role
func (sm PostgresRoleManager) Delete(ctx context.Context, role v1.RoleConfiguration) error {
	contextLog := log.FromContext(ctx).WithName("dropRole")
	contextLog.Trace("Invoked", "role", role)
	return nil
}
