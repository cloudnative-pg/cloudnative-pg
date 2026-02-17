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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

type roleInheritanceManager struct {
	origin      pool.Pooler
	destination pool.Pooler
}

// RoleInheritance contains the data needed to execute grants, based on pg_authid
type RoleInheritance struct {
	RoleID      string  `json:"roleid,omitempty"`
	Member      string  `json:"member,omitempty"`
	AdminOption bool    `json:"admin_option,omitempty"`
	Grantor     *string `json:"grantor,omitempty"`
}

func cloneRoleInheritance(ctx context.Context, destination pool.Pooler, origin pool.Pooler) error {
	rs := roleInheritanceManager{
		origin:      origin,
		destination: destination,
	}

	ri, err := rs.getRoleInheritance(ctx)
	if err != nil {
		return err
	}

	return rs.importRoleInheritance(ctx, ri)
}

func (rs *roleInheritanceManager) importRoleInheritance(ctx context.Context, ris []RoleInheritance) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("importing role inheritances")

	db, err := rs.destination.Connection(postgresDatabase)
	if err != nil {
		return err
	}

	for _, inheritance := range ris {
		query := fmt.Sprintf("GRANT %s TO %s ",
			pgx.Identifier{inheritance.RoleID}.Sanitize(),
			pgx.Identifier{inheritance.Member}.Sanitize())
		if inheritance.AdminOption {
			query += "WITH ADMIN OPTION "
		}
		if inheritance.Grantor != nil {
			query += fmt.Sprintf("GRANTED BY %s",
				pgx.Identifier{*inheritance.Grantor}.Sanitize())
		}

		contextLogger.Info("executing role inheritance query", "query", query)

		_, err := db.Exec(query)
		if err != nil {
			contextLogger.Error(err, "while importing role inheritance")
		}
	}

	return nil
}

func (rs *roleInheritanceManager) getRoleInheritance(ctx context.Context) ([]RoleInheritance, error) {
	contextLogger := log.FromContext(ctx)
	originDB, err := rs.origin.Connection(postgresDatabase)
	if err != nil {
		return nil, err
	}

	// Retrieve the roles inheritance excluding roles that are owned by the postgres catalog
	// see FirstNormalObjectId in https://github.com/postgres/postgres/blob/662dbe2/src/include/access/transam.h#L197
	query := "SELECT ur.rolname AS roleid, " +
		"um.rolname AS member, " +
		"a.admin_option, " +
		"ug.rolname AS grantor " +
		"FROM pg_catalog.pg_auth_members a " +
		"LEFT JOIN pg_catalog.pg_authid ur on ur.oid = a.roleid " +
		"LEFT JOIN pg_catalog.pg_authid um on um.oid = a.member " +
		"LEFT JOIN pg_catalog.pg_authid ug on ug.oid = a.grantor " +
		"WHERE ur.oid >= 16384 AND um.oid >= 16384"

	rows, err := originDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			contextLogger.Error(closeErr, "while closing rows")
		}
	}()

	var ris []RoleInheritance
	for rows.Next() {
		var ri RoleInheritance
		if err := rows.Scan(
			&ri.RoleID,
			&ri.Member,
			&ri.AdminOption,
			&ri.Grantor,
		); err != nil {
			return nil, err
		}

		ris = append(ris, ri)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ris, nil
}
