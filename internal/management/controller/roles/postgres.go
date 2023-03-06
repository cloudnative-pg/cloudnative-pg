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

	"github.com/jackc/pgx/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// PostgresRoleManager is a RoleManager for a database instance
type PostgresRoleManager struct {
	superUserDB *sql.DB
	client      ctrl.Client
	instance    *postgres.Instance
}

// NewPostgresRoleManager returns an implementation of RoleManager for postgres
func NewPostgresRoleManager(superDB *sql.DB, client ctrl.Client, instance *postgres.Instance) RoleManager {
	return PostgresRoleManager{
		superUserDB: superDB,
		client:      client,
		instance:    instance,
	}
}

// List the available roles
func (sm PostgresRoleManager) List(
	ctx context.Context,
	config *v1.ManagedConfiguration,
) ([]v1.RoleConfiguration, error) {
	rows, err := sm.superUserDB.QueryContext(
		ctx,
		`SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, 
       			rolcanlogin, rolreplication, rolconnlimit, rolpassword, rolvaliduntil, rolbypassrls,
		FROM pg_catalog.pg_authid where rolname not like 'pg_%';`)
	if err != nil {
		return []v1.RoleConfiguration{}, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var roles []v1.RoleConfiguration
	for rows.Next() {
		var validuntil sql.NullString
		var role v1.RoleConfiguration
		err := rows.Scan(
			&role.Name,
			&role.Superuser,
			&role.Inherit,
			&role.CreateRole,
			&role.CreateDB,
			&role.Login,
			&role.Replication,
			&role.ConnectionLimit,
			&role.Password,
			&validuntil,
			&role.BypassRLS,
		)
		if err != nil {
			return []v1.RoleConfiguration{}, err
		}
		if validuntil.Valid {
			role.ValidUntil = validuntil.String
		}

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
	query.WriteString(fmt.Sprintf("ALTER ROLE %s ", pgx.Identifier{role.Name}.Sanitize()))
	appendRoleOptions(role, &query)
	err := sm.appendPasswordOption(ctx, role, &query)
	if err != nil {
		return fmt.Errorf("could not create role %s: %w ", role.Name, err)
	}

	contextLog.Info("Updating", "query", query.String())

	result, err := sm.superUserDB.ExecContext(ctx, query.String())
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not update role %s: %w", role.Name, err)
	}

	return nil
}

// Create the role
// TODO: do we give the role any database-level permissions?
func (sm PostgresRoleManager) Create(ctx context.Context, role v1.RoleConfiguration) error {
	contextLog := log.FromContext(ctx).WithName("createRole")
	contextLog.Trace("Invoked", "role", role)

	var query strings.Builder
	query.WriteString(fmt.Sprintf("CREATE ROLE %s ", pgx.Identifier{role.Name}.Sanitize()))
	appendRoleOptions(role, &query)
	err := sm.appendPasswordOption(ctx, role, &query)
	if err != nil {
		return fmt.Errorf("could not create role %s: %w ", role.Name, err)
	}

	contextLog.Info("Creating", "query", query.String())
	// NOTE: defensively we might think of doint CREATE ... IF EXISTS
	// but at least during development, we want to catch the error
	// Even after, this may be "the kubernetes way"
	_, err = sm.superUserDB.ExecContext(ctx, query.String())
	if err != nil {
		return fmt.Errorf("could not create role %s: %w ", role.Name, err)
	}

	return nil
}

// Delete the role
// TODO: we need to do something better here. We should not delete a user that
// has created tables or other objects. That should be blocked at the validation
// webhook level, otherwise it will be very poor UX and the operator may not notice
func (sm PostgresRoleManager) Delete(ctx context.Context, role v1.RoleConfiguration) error {
	contextLog := log.FromContext(ctx).WithName("dropRole")
	contextLog.Trace("Invoked", "role", role)

	_, err := sm.superUserDB.ExecContext(ctx, fmt.Sprintf("DROP ROLE %s", pgx.Identifier{role.Name}.Sanitize()))
	if err != nil {
		return fmt.Errorf("could not delete role %s: %w", role.Name, err)
	}

	return nil
}

func appendRoleOptions(role v1.RoleConfiguration, query *strings.Builder) {
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

	if role.CreateRole {
		query.WriteString("CREATEROLE ")
	} else {
		query.WriteString("NOCREATEROLE ")
	}

	if role.Inherit {
		query.WriteString("INHERIT ")
	} else {
		query.WriteString("NOINHERIT ")
	}

	if role.Login {
		query.WriteString("LOGIN ")
	} else {
		query.WriteString("NOLOGIN ")
	}

	if role.Replication {
		query.WriteString("REPLICATION ")
	} else {
		query.WriteString("NOREPLICATION ")
	}

	if role.Superuser {
		query.WriteString("SUPERUSER ")
	} else {
		query.WriteString("NOSUPERUSER ")
	}

	if role.ConnectionLimit > -1 {
		query.WriteString(fmt.Sprintf("CONNECTION LIMIT %d ", role.ConnectionLimit))
	}
}

func (sm PostgresRoleManager) appendPasswordOption(ctx context.Context,
	role v1.RoleConfiguration,
	query *strings.Builder,
) error {
	if role.PasswordSecret == nil {
		return nil
	}
	secretName := role.PasswordSecret.Name
	var secret corev1.Secret
	err := sm.client.Get(
		ctx,
		ctrl.ObjectKey{Namespace: sm.instance.Namespace, Name: secretName},
		&secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	usernameFromSecret, password, err := utils.GetUserPasswordFromSecret(&secret)
	if err != nil {
		return err
	}
	if role.Name != usernameFromSecret {
		return fmt.Errorf("wrong username '%v' in secret, expected '%v'", usernameFromSecret, role.Name)
	}
	if password == "" {
		query.WriteString(fmt.Sprintf("PASSWORD %s", password))
	} else {
		query.WriteString("PASSWORD NULL")
	}
	return nil
}
