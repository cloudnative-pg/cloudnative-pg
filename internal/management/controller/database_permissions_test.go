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
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Database permissions", func() {
	grant := func(name string) apiv1.UsageSpec {
		return apiv1.UsageSpec{Name: name, Type: apiv1.GrantUsageSpecType}
	}
	revoke := func(name string) apiv1.UsageSpec {
		return apiv1.UsageSpec{Name: name, Type: apiv1.RevokeUsageSpecType}
	}

	// the ACL of a database that has never been customized, owned by the role 10
	defaultACL := databaseACL{
		{grantee: 0, privilege: "CONNECT"}:    {},
		{grantee: 0, privilege: "TEMPORARY"}:  {},
		{grantee: 10, privilege: "CONNECT"}:   {},
		{grantee: 10, privilege: "CREATE"}:    {},
		{grantee: 10, privilege: "TEMPORARY"}: {},
	}

	statements := func(changes []databasePermissionChange) []string {
		var result []string
		for _, change := range changes {
			if change.statement != "" {
				result = append(result, change.statement)
			}
		}
		return result
	}

	Describe("expandDatabasePermissions", func() {
		It("returns nothing without permissions", func() {
			Expect(expandDatabasePermissions(nil)).To(BeEmpty())
		})

		It("defaults to grant and sorts by privilege and role", func() {
			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect:   []apiv1.UsageSpec{{Name: "zeta"}, revoke("public"), grant("app")},
				Temporary: []apiv1.UsageSpec{revoke("app")},
			})
			Expect(changes).To(HaveLen(4))
			Expect([]string{changes[0].name, changes[1].name, changes[2].name}).
				To(Equal([]string{"app", "public", "zeta"}))
			Expect(changes[2].changeType).To(Equal(apiv1.GrantUsageSpecType))
			Expect(changes[3].privilege).To(Equal("TEMPORARY"))
		})

		It("expands all into the three privileges", func() {
			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect([]string{changes[0].privilege, changes[1].privilege, changes[2].privilege}).
				To(Equal([]string{"CONNECT", "CREATE", "TEMPORARY"}))
		})

		It("flags an entry that is repeated, as the webhook could have been bypassed", func() {
			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), grant("app")},
				All:     []apiv1.UsageSpec{grant("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect(changes[0].err).To(MatchError(errRepeatedDatabasePermission))
			Expect(changes[1].err).ToNot(HaveOccurred())
			Expect(changes[2].err).ToNot(HaveOccurred())
		})

		It("flags conflicting entries, as the webhook could have been bypassed", func() {
			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{revoke("app")},
				All:     []apiv1.UsageSpec{grant("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect(changes[0].err).To(MatchError(errConflictingDatabasePermission))
			Expect(changes[1].err).ToNot(HaveOccurred())
		})
	})

	Describe("planDatabasePermissions", func() {
		// the OID of the roles that exist, as it would be found by decorating
		roles := map[string]int64{"app": 20, "PUBLIC": 30}

		decorated := func(
			permissions *apiv1.DatabasePermissionsSpec,
			oids map[string]int64,
		) []databasePermissionChange {
			changes := expandDatabasePermissions(permissions)
			for i := range changes {
				if changes[i].name != "public" {
					changes[i].granteeOid = oids[changes[i].name]
				}
			}
			return changes
		}

		plan := func(acl databaseACL, permissions *apiv1.DatabasePermissionsSpec) []databasePermissionChange {
			return planDatabasePermissions("my-db", decorated(permissions, roles), acl)
		}

		It("revokes CONNECT from PUBLIC and grants it back to a role", func() {
			changes := plan(defaultACL, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{revoke("public"), grant("app")},
			})
			Expect(statements(changes)).To(ConsistOf(
				`GRANT CONNECT ON DATABASE "my-db" TO "app"`,
				`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`,
			))
		})

		It("sees the default ACL of a database with a NULL datacl", func() {
			// Revoking from PUBLIC must not be skipped just because the
			// stored ACL is NULL: the caller expands the defaults.
			changes := plan(defaultACL, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{revoke("public")},
			})
			Expect(statements(changes)).To(HaveLen(1))
		})

		It("emits nothing when the ACL already matches", func() {
			acl := databaseACL{
				{grantee: 20, privilege: "CONNECT"}: {},
			}
			changes := plan(acl, &apiv1.DatabasePermissionsSpec{
				Connect:   []apiv1.UsageSpec{grant("app"), revoke("public")},
				Temporary: []apiv1.UsageSpec{revoke("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect(statements(changes)).To(BeEmpty())
		})

		It("expands all", func() {
			changes := plan(databaseACL{}, &apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(statements(changes)).To(Equal([]string{
				`GRANT CONNECT ON DATABASE "my-db" TO "app"`,
				`GRANT CREATE ON DATABASE "my-db" TO "app"`,
				`GRANT TEMPORARY ON DATABASE "my-db" TO "app"`,
			}))
		})

		It("treats only the exact lowercase public as PUBLIC", func() {
			changes := plan(databaseACL{}, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("PUBLIC")},
			})
			// "PUBLIC" is an ordinary role that happens to exist in this test
			Expect(statements(changes)).To(Equal([]string{
				`GRANT CONNECT ON DATABASE "my-db" TO "PUBLIC"`,
			}))
		})

		It("quotes the database and the role names", func() {
			permissions := &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("Ro\"le")},
			}
			changes := planDatabasePermissions("we\"ird",
				decorated(permissions, map[string]int64{"Ro\"le": 1}), databaseACL{})
			Expect(statements(changes)).To(Equal([]string{
				`GRANT CONNECT ON DATABASE "we""ird" TO "Ro""le"`,
			}))
		})

		It("leaves untouched the changes that already failed", func() {
			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("ghost")},
			})
			changes[0].err = errDatabasePermissionRoleNotFound

			planned := planDatabasePermissions("my-db", changes, defaultACL)
			Expect(statements(planned)).To(BeEmpty())
			Expect(planned[0].err).To(MatchError(errDatabasePermissionRoleNotFound))
		})
	})

	Describe("decorateDatabasePermissions", func() {
		var (
			dbMock sqlmock.Sqlmock
			db     *sql.DB
		)

		const roleQuery = "SELECT oid::bigint FROM pg_catalog.pg_roles WHERE rolname = $1"

		BeforeEach(func() {
			var err error
			db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		})

		It("sets the OID of the roles, 0 for public without querying it", func(ctx SpecContext) {
			dbMock.ExpectQuery(roleQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))

			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			Expect(changes[0].granteeOid).To(BeEquivalentTo(20))
			Expect(changes[1].granteeOid).To(BeEquivalentTo(0))
			Expect(changes[0].err).ToNot(HaveOccurred())
			Expect(changes[1].err).ToNot(HaveOccurred())
		})

		It("looks up a role only once, even if it has several privileges", func(ctx SpecContext) {
			dbMock.ExpectQuery(roleQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))

			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			for _, change := range changes {
				Expect(change.granteeOid).To(BeEquivalentTo(20))
			}
		})

		It("marks the roles that do not exist, for grants and revokes", func(ctx SpecContext) {
			dbMock.ExpectQuery(roleQuery).WithArgs("ghost").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))
			dbMock.ExpectQuery(roleQuery).WithArgs("phantom").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))

			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("ghost"), revoke("phantom")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			Expect(changes[0].err).To(MatchError(errDatabasePermissionRoleNotFound))
			Expect(changes[1].err).To(MatchError(errDatabasePermissionRoleNotFound))
		})

		It("does not look up the roles of the changes that already failed", func(ctx SpecContext) {
			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app")},
			})
			changes[0].err = errRepeatedDatabasePermission

			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			Expect(changes[0].err).To(MatchError(errRepeatedDatabasePermission))
		})

		It("returns the error of a failing lookup", func(ctx SpecContext) {
			dbMock.ExpectQuery(roleQuery).WithArgs("app").WillReturnError(errors.New("boom"))

			changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(MatchError(ContainSubstring("boom")))
		})
	})

	Describe("reconcileDatabasePermissions", func() {
		var (
			dbMock sqlmock.Sqlmock
			db     *sql.DB
		)

		const aclQuery = `SELECT a.grantee::bigint, a.privilege_type
		FROM pg_catalog.pg_database d,
		     pg_catalog.aclexplode(coalesce(d.datacl, pg_catalog.acldefault('d', d.datdba))) a
		WHERE d.datname = $1 AND a.grantor = d.datdba`
		const roleQuery = "SELECT oid::bigint FROM pg_catalog.pg_roles WHERE rolname = $1"

		reconcile := func(ctx SpecContext, permissions *apiv1.DatabasePermissionsSpec) []apiv1.DatabasePermissionStatus {
			return reconcileDatabasePermissions(ctx, db, &apiv1.Database{
				Spec: apiv1.DatabaseSpec{Name: "my-db", Permissions: permissions},
			})
		}

		BeforeEach(func() {
			var err error
			db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		})

		expectDefaultACL := func() {
			dbMock.ExpectQuery(aclQuery).WithArgs("my-db").WillReturnRows(
				sqlmock.NewRows([]string{"grantee", "privilege_type"}).
					AddRow(0, "CONNECT").AddRow(0, "TEMPORARY").
					AddRow(10, "CONNECT").AddRow(10, "CREATE").AddRow(10, "TEMPORARY"))
		}

		It("does nothing without permissions", func(ctx SpecContext) {
			Expect(reconcile(ctx, nil)).To(BeEmpty())
		})

		It("applies the changes and reports every entry", func(ctx SpecContext) {
			expectDefaultACL()
			dbMock.ExpectQuery(roleQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			dbMock.ExpectExec(`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{revoke("public"), grant("app")},
			})
			Expect(statuses).To(Equal([]apiv1.DatabasePermissionStatus{
				{Privilege: "CONNECT", Name: "app", Type: apiv1.GrantUsageSpecType, Applied: true},
				{Privilege: "CONNECT", Name: "public", Type: apiv1.RevokeUsageSpecType, Applied: true},
			}))
		})

		It("applies the other entries when a role does not exist", func(ctx SpecContext) {
			expectDefaultACL()
			dbMock.ExpectQuery(roleQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			dbMock.ExpectQuery(roleQuery).WithArgs("ghost").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), grant("ghost")},
			})
			Expect(statuses).To(HaveLen(2))
			Expect(statuses[0].Applied).To(BeTrue())
			Expect(statuses[1].Applied).To(BeFalse())
			Expect(statuses[1].Message).To(Equal("role does not exist"))
		})

		It("reports a failing statement against its entry only", func(ctx SpecContext) {
			expectDefaultACL()
			dbMock.ExpectQuery(roleQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnError(errors.New("boom"))
			dbMock.ExpectExec(`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(statuses[0].Applied).To(BeFalse())
			Expect(statuses[0].Message).To(Equal("boom"))
			Expect(statuses[1].Applied).To(BeTrue())
		})

		It("reports the failure to read the ACL against every entry", func(ctx SpecContext) {
			dbMock.ExpectQuery(aclQuery).WithArgs("my-db").WillReturnError(errors.New("no connection"))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(statuses).To(HaveLen(3))
			for _, status := range statuses {
				Expect(status.Applied).To(BeFalse())
				Expect(status.Message).To(ContainSubstring("no connection"))
			}
		})

		It("is idempotent: nothing is executed when the ACL matches", func(ctx SpecContext) {
			dbMock.ExpectQuery(aclQuery).WithArgs("my-db").WillReturnRows(
				sqlmock.NewRows([]string{"grantee", "privilege_type"}).AddRow(20, "CONNECT"))
			dbMock.ExpectQuery(roleQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(statuses).To(HaveLen(2))
			for _, status := range statuses {
				Expect(status.Applied).To(BeTrue())
			}
		})
	})
})
