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

package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// publicGranteeOid is the OID that identifies PUBLIC in an ACL item
const publicGranteeOid int64 = 0

// databasePrivileges lists the privileges that can be managed on a database
const (
	connectPrivilege   = "CONNECT"
	createPrivilege    = "CREATE"
	temporaryPrivilege = "TEMPORARY"
)

var (
	// The errors below are reported in the status entry of the permission,
	// which already identifies the privilege and the role.

	// errConflictingDatabasePermission is raised when a role is both granted
	// and revoked the same privilege
	errConflictingDatabasePermission = errors.New("the role is both granted and revoked this privilege")

	// errRepeatedDatabasePermission is raised when a role is listed more
	// than once for the same privilege
	errRepeatedDatabasePermission = errors.New("the role is repeated for this privilege")

	// errDatabasePermissionRoleNotFound is raised when the role of a
	// permission doesn't exist in PostgreSQL
	errDatabasePermissionRoleNotFound = errors.New("role does not exist")
)

// databaseACLKey identifies an explicit privilege held by a grantee in the
// ACL of a database
type databaseACLKey struct {
	grantee   int64
	privilege string
}

// databaseACL is the set of privileges that were granted by the database owner
type databaseACL map[databaseACLKey]struct{}

// databasePermissionChange is the outcome of comparing a single requested
// privilege change with the current ACL of the database. The `all` shortcut
// is expanded before comparing, so there is an entry for each
// privilege and role.
type databasePermissionChange struct {
	privilege  string
	name       string
	changeType apiv1.UsageSpecType

	// granteeOid is the OID that identifies the role in the ACL of the
	// database, which is 0 for PUBLIC. It is valid only once the change has
	// been decorated.
	granteeOid int64

	// statement is the SQL to execute, empty when the ACL already matches
	statement string

	// err is set when the change cannot be computed, such as when the
	// role doesn't exist
	err error
}

// expandDatabasePermissions expands the `all` shortcut and returns the
// requested change for every privilege and role, deduplicated and sorted
// by privilege and role name so that the result is deterministic.
func expandDatabasePermissions(
	permissions *apiv1.DatabasePermissionsSpec,
) []databasePermissionChange {
	if permissions == nil {
		return nil
	}

	lists := []struct {
		privilege string
		entries   []apiv1.UsageSpec
	}{
		{connectPrivilege, slices.Concat(permissions.Connect, permissions.All)},
		{createPrivilege, slices.Concat(permissions.Create, permissions.All)},
		{temporaryPrivilege, slices.Concat(permissions.Temporary, permissions.All)},
	}

	var result []databasePermissionChange
	for _, list := range lists {
		seen := make(map[string]int, len(list.entries))
		start := len(result)
		for _, entry := range list.entries {
			changeType := entry.Type
			if changeType == "" {
				changeType = apiv1.GrantUsageSpecType
			}

			if i, found := seen[granteeKey(entry.Name)]; found {
				// The webhook rejects both cases, but it can be bypassed
				if result[i].changeType != changeType {
					result[i].err = errConflictingDatabasePermission
				} else {
					result[i].err = errRepeatedDatabasePermission
				}
				continue
			}

			seen[granteeKey(entry.Name)] = len(result)
			result = append(result, databasePermissionChange{
				privilege:  list.privilege,
				name:       entry.Name,
				changeType: changeType,
			})
		}

		added := result[start:]
		sort.SliceStable(added, func(i, j int) bool { return added[i].name < added[j].name })
	}

	return result
}

// granteeKey returns the key that identifies a grantee, so that every spelling
// of `public` resolves to the PUBLIC pseudo-role, as `sanitizeGrantee` does
func granteeKey(name string) string {
	if strings.EqualFold(name, publicRole) {
		return publicRole
	}
	return name
}

// planDatabasePermissions compares the decorated changes with the ACL of the
// database and fills in the statement that is needed to apply each one.
//
// Only the privileges granted by the database owner are considered by
// `acl`, as the owner is the grantor recorded by PostgreSQL when a superuser
// runs GRANT and REVOKE.
func planDatabasePermissions(
	databaseName string,
	changes []databasePermissionChange,
	acl databaseACL,
) []databasePermissionChange {
	result := make([]databasePermissionChange, 0, len(changes))
	for _, change := range changes {
		if change.err != nil {
			result = append(result, change)
			continue
		}

		_, granted := acl[databaseACLKey{grantee: change.granteeOid, privilege: change.privilege}]
		switch {
		case change.changeType == apiv1.GrantUsageSpecType && !granted:
			change.statement = fmt.Sprintf("GRANT %s ON DATABASE %s TO %s",
				change.privilege,
				pgx.Identifier{databaseName}.Sanitize(),
				sanitizeGrantee(change.name))
		case change.changeType == apiv1.RevokeUsageSpecType && granted:
			change.statement = fmt.Sprintf("REVOKE %s ON DATABASE %s FROM %s",
				change.privilege,
				pgx.Identifier{databaseName}.Sanitize(),
				sanitizeGrantee(change.name))
		}

		result = append(result, change)
	}

	return result
}

// getDatabaseACL reads the privileges that the owner granted on the database.
// A NULL `datacl` means that the default ACL applies, which is expanded here
// so that PUBLIC's default CONNECT and TEMPORARY privileges are visible.
func getDatabaseACL(ctx context.Context, db *sql.DB, databaseName string) (databaseACL, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT a.grantee::bigint, a.privilege_type
		FROM pg_catalog.pg_database d,
		     pg_catalog.aclexplode(coalesce(d.datacl, pg_catalog.acldefault('d', d.datdba))) a
		WHERE d.datname = $1 AND a.grantor = d.datdba`,
		databaseName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	acl := make(databaseACL)
	for rows.Next() {
		var key databaseACLKey
		if err := rows.Scan(&key.grantee, &key.privilege); err != nil {
			return nil, err
		}
		acl[key] = struct{}{}
	}

	return acl, rows.Err()
}

// getRoleOid returns the OID of the role with the given name, which is matched
// exactly. The boolean is false if the role doesn't exist.
func getRoleOid(ctx context.Context, db *sql.DB, roleName string) (int64, bool, error) {
	var oid int64
	err := db.QueryRowContext(ctx,
		"SELECT oid::bigint FROM pg_catalog.pg_roles WHERE rolname = $1",
		roleName).Scan(&oid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return oid, err == nil, err
}

// decorateDatabasePermissions sets the OID of the grantee of each change,
// marking with an error the changes whose role doesn't exist. The changes are
// updated in place.
func decorateDatabasePermissions(
	ctx context.Context,
	db *sql.DB,
	changes []databasePermissionChange,
) error {
	type roleLookup struct {
		oid   int64
		found bool
	}

	// Look up only the roles named in the specification, instead of reading the
	// whole pg_roles view: a cluster with thousands of roles would pay for that
	// on every reconciliation, to resolve a handful of names. A role can be
	// listed for several privileges, so each one is looked up once.
	lookups := make(map[string]roleLookup)
	for i := range changes {
		change := &changes[i]
		if change.err != nil {
			continue
		}

		if granteeKey(change.name) == publicRole {
			change.granteeOid = publicGranteeOid
			continue
		}

		lookup, known := lookups[change.name]
		if !known {
			var err error
			lookup.oid, lookup.found, err = getRoleOid(ctx, db, change.name)
			if err != nil {
				return fmt.Errorf("while looking up the role %q: %w", change.name, err)
			}
			lookups[change.name] = lookup
		}

		if !lookup.found {
			change.err = errDatabasePermissionRoleNotFound
			continue
		}
		change.granteeOid = lookup.oid
	}

	return nil
}

// computeDatabasePermissionsPlan reads the current state of the database and
// computes the statements that are needed. On error, the returned plan still
// lists every change so the caller can report the failure against each of them.
func computeDatabasePermissionsPlan(
	ctx context.Context,
	db *sql.DB,
	databaseName string,
	changes []databasePermissionChange,
) ([]databasePermissionChange, error) {
	acl, err := getDatabaseACL(ctx, db, databaseName)
	if err != nil {
		return changes, fmt.Errorf("while reading the permissions of database %q: %w", databaseName, err)
	}

	if err := decorateDatabasePermissions(ctx, db, changes); err != nil {
		return changes, err
	}

	return planDatabasePermissions(databaseName, changes, acl), nil
}

// reconcileDatabasePermissions makes the ACL of the database match the
// requested permissions and returns the outcome of each entry. The entries are
// independent: one failing doesn't prevent the others from being applied.
func reconcileDatabasePermissions(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) []apiv1.DatabasePermissionStatus {
	databaseName := obj.Spec.Name

	changes := expandDatabasePermissions(obj.Spec.Permissions)
	if len(changes) == 0 {
		return nil
	}

	plan, err := computeDatabasePermissionsPlan(ctx, db, databaseName, changes)
	if err != nil {
		for i := range plan {
			plan[i].err = err
		}
	}

	contextLogger := log.FromContext(ctx)
	statuses := make([]apiv1.DatabasePermissionStatus, 0, len(plan))
	for _, change := range plan {
		if change.err == nil && change.statement != "" {
			if _, err := db.ExecContext(ctx, change.statement); err != nil {
				change.err = err
			} else {
				contextLogger.Info("applied database permission",
					"database", databaseName,
					"privilege", change.privilege,
					"type", change.changeType,
					"grantee", change.name)
			}
		}

		status := apiv1.DatabasePermissionStatus{
			Privilege: change.privilege,
			Name:      change.name,
			Type:      change.changeType,
			Applied:   change.err == nil,
		}
		if change.err != nil {
			status.Message = change.err.Error()
		}
		statuses = append(statuses, status)
	}

	return statuses
}
