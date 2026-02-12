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

package logicalimport

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/blang/semver"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
)

type roleManager struct {
	origin      pool.Pooler
	destination pool.Pooler
	cluster     *apiv1.Cluster
}

// Role a result from pg_authid table
type Role struct {
	Oid            string  `json:"oid,omitempty"`
	Rolname        string  `json:"rolname,omitempty"`
	Rolsuper       bool    `json:"rolsuper,omitempty"`
	Rolinherit     bool    `json:"rolinherit,omitempty"`
	Rolcreaterole  bool    `json:"rolcreaterole,omitempty"`
	Rolcreatedb    bool    `json:"rolcreatedb,omitempty"`
	Rolcanlogin    bool    `json:"rolcanlogin,omitempty"`
	Rolreplication bool    `json:"rolreplication,omitempty"`
	Rolbypassrls   bool    `json:"rolbypassrls,omitempty"`
	IsCurrentUser  bool    `json:"is_current_user,omitempty"`
	Rolconnlimit   int     `json:"rolconnlimit,omitempty"`
	Rolpassword    *string `json:"rolpassword,omitempty"`
	Rolvaliduntil  *string `json:"rolvaliduntil,omitempty"`
	RolComment     *string `json:"rolcomment,omitempty"`
}

func cloneRoles(
	ctx context.Context,
	cluster *apiv1.Cluster,
	destination pool.Pooler,
	origin pool.Pooler,
) error {
	rs := roleManager{origin: origin, destination: destination, cluster: cluster}
	roles, err := rs.getRoles(ctx)
	if err != nil {
		return err
	}

	return rs.importRoles(ctx, roles)
}

func (rs *roleManager) importRoles(ctx context.Context, roles []Role) error {
	contextLogger := log.FromContext(ctx)

	db, err := rs.destination.Connection(postgresDatabase)
	if err != nil {
		return err
	}
	for _, role := range roles {
		query := rs.createSQLStatement(role)
		contextLogger.Info("executing import role query", "query", query)
		_, err := db.Exec(query)
		if err != nil {
			contextLogger.Error(err, "error while importing the role")

			continue
		}
	}
	return nil
}

func (rs *roleManager) createSQLStatement(role Role) string {
	query := fmt.Sprintf("CREATE ROLE %s WITH ", pgx.Identifier{role.Rolname}.Sanitize())

	if role.Rolcreatedb {
		query += "CREATEDB "
	}

	if role.Rolsuper {
		query += "SUPERUSER "
	}

	if role.Rolcanlogin {
		query += "LOGIN "
	} else {
		query += "NOLOGIN "
	}

	if role.Rolinherit {
		query += "INHERIT "
	}

	if role.Rolcreaterole {
		query += "CREATEROLE "
	}

	if role.Rolbypassrls {
		query += "BYPASSRLS "
	}

	if role.Rolvaliduntil != nil {
		query += fmt.Sprintf("VALID UNTIL %s ", pq.QuoteLiteral(*role.Rolvaliduntil))
	}

	if role.Rolpassword != nil {
		query += fmt.Sprintf("PASSWORD %s ", pq.QuoteLiteral(*role.Rolpassword))
	}
	query += fmt.Sprintf("CONNECTION LIMIT %d", role.Rolconnlimit)

	return query
}

func (rs *roleManager) getRoles(ctx context.Context) ([]Role, error) {
	contextLogger := log.FromContext(ctx)
	originDatabase, err := rs.origin.Connection(postgresDatabase)
	if err != nil {
		return nil, err
	}
	vers, err := utils.GetPgVersion(originDatabase)
	if err != nil {
		return nil, err
	}
	contextLogger.Info("postgres version extracted", "version", vers.String())

	var rows *sql.Rows
	var query string

	// Retrieve the roles excluding those that are owned by the postgres catalog
	// see FirstNormalObjectId in https://github.com/postgres/postgres/blob/662dbe2/src/include/access/transam.h#L197
	if vers.GTE(semver.Version{Major: 9, Minor: 5}) {
		query = "SELECT oid, rolname, rolsuper, rolinherit, " +
			"rolcreaterole, rolcreatedb, " +
			"rolcanlogin, rolconnlimit, rolpassword, " +
			"rolvaliduntil, rolreplication, rolbypassrls, " +
			"pg_catalog.shobj_description(oid, 'pg_authid') as rolcomment, " +
			"rolname = CURRENT_USER AS is_current_user " +
			"FROM pg_catalog.pg_authid " +
			"WHERE oid >= 16384 " +
			"ORDER BY 2"
	} else {
		query = "SELECT oid, rolname, rolsuper, rolinherit, " +
			"rolcreaterole, rolcreatedb, " +
			"rolcanlogin, rolconnlimit, rolpassword, " +
			"rolvaliduntil, rolreplication, " +
			"false as rolbypassrls, " +
			"pg_catalog.shobj_description(oid, 'pg_authid') as rolcomment, " +
			"rolname = CURRENT_USER AS is_current_user " +
			"FROM pg_catalog.pg_authid " +
			"WHERE oid >= 16384 " +
			"ORDER BY 2"
	}

	contextLogger.Debug("executing role snapshot query", "query", query)
	rows, err = originDatabase.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			contextLogger.Error(closeErr, "while closing rows")
		}
	}()

	rolesToImport := rs.cluster.Spec.Bootstrap.InitDB.Import.Roles
	rolesToSkip := []string{
		"postgres",
		apiv1.StreamingReplicationUser,
		apiv1.PGBouncerPoolerUserName,
		rs.cluster.Spec.Bootstrap.InitDB.Owner,
	}

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(
			&r.Oid,
			&r.Rolname,
			&r.Rolsuper,
			&r.Rolinherit,
			&r.Rolcreaterole,
			&r.Rolcreatedb,
			&r.Rolcanlogin,
			&r.Rolconnlimit,
			&r.Rolpassword,
			&r.Rolvaliduntil,
			&r.Rolreplication,
			&r.Rolbypassrls,
			&r.RolComment,
			&r.IsCurrentUser,
		); err != nil {
			return nil, err
		}

		if slices.Contains(rolesToSkip, r.Rolname) {
			contextLogger.Info(
				"found a role that needs to be skipped",
				"rolesToSkip", rolesToSkip,
				"role", r,
			)
			continue
		}

		if !shouldImportRole(r.Rolname, rolesToImport) {
			contextLogger.Info(
				"found a role that doesn't need to be imported",
				"rolesToImport", rolesToImport,
				"role", r,
			)
			continue
		}

		if r.Rolsuper {
			contextLogger.Debug(
				"found a superUser, downgrading permissions",
				"role", r,
			)
			r.Rolsuper = false
		}

		roles = append(roles, r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return roles, nil
}

func shouldImportRole(rolname string, rolesToImport []string) bool {
	if slices.Contains(rolesToImport, "*") {
		return true
	}

	if slices.Contains(rolesToImport, rolname) {
		return true
	}

	return false
}
