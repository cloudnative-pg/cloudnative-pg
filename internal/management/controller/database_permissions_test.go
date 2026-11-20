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
	"slices"

	"github.com/DATA-DOG/go-sqlmock"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The SQL is copied here on purpose, so that changing the queries of
// database_permissions.go by mistake makes the tests fail.
const (
	expectedACLQuery = `SELECT a.grantee::bigint, a.privilege_type
		FROM pg_catalog.pg_database d,
		     pg_catalog.aclexplode(coalesce(d.datacl, pg_catalog.acldefault('d', d.datdba))) a
		WHERE d.datname = $1 AND a.grantor = d.datdba`

	expectedRoleOidQuery = "SELECT oid::bigint FROM pg_catalog.pg_roles WHERE rolname = $1"
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

	// expandUserPermissions expands the permissions without the grant to the
	// metrics exporter that the operator adds, to test the entries of the user
	expandUserPermissions := func(permissions *apiv1.DatabasePermissionsSpec) []databasePermissionChange {
		return slices.DeleteFunc(expandDatabasePermissions(permissions), func(change databasePermissionChange) bool {
			return change.name == apiv1.MetricsExporterUserName
		})
	}

	Describe("expandDatabasePermissions", func() {
		It("returns nothing without permissions", func() {
			Expect(expandUserPermissions(nil)).To(BeEmpty())
		})

		It("defaults to grant and keeps the order of the specification", func() {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect:   []apiv1.UsageSpec{{Name: "zeta"}, revoke("public"), grant("app")},
				Temporary: []apiv1.UsageSpec{revoke("app")},
			})
			Expect(changes).To(HaveLen(4))
			Expect([]string{changes[0].name, changes[1].name, changes[2].name}).
				To(Equal([]string{"zeta", "public", "app"}))
			Expect(changes[0].changeType).To(Equal(apiv1.GrantUsageSpecType))
			Expect(changes[3].privilege).To(Equal("TEMPORARY"))
		})

		It("applies the entries of all after the ones of the privilege", func() {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				All:     []apiv1.UsageSpec{grant("from-all")},
				Connect: []apiv1.UsageSpec{grant("from-connect")},
			})
			Expect(changes).To(HaveLen(4))
			Expect([]string{changes[0].privilege, changes[0].name}).To(Equal([]string{"CONNECT", "from-connect"}))
			Expect([]string{changes[1].privilege, changes[1].name}).To(Equal([]string{"CONNECT", "from-all"}))
		})

		It("expands all into the three privileges", func() {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect([]string{changes[0].privilege, changes[1].privilege, changes[2].privilege}).
				To(Equal([]string{"CONNECT", "CREATE", "TEMPORARY"}))
		})

		It("flags an entry that is repeated, as the webhook could have been bypassed", func() {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), grant("app")},
				All:     []apiv1.UsageSpec{grant("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect(changes[0].err).To(MatchError(errRepeatedDatabasePermission))
			Expect(changes[1].err).ToNot(HaveOccurred())
			Expect(changes[2].err).ToNot(HaveOccurred())
		})

		It("flags conflicting entries, as the webhook could have been bypassed", func() {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{revoke("app")},
				All:     []apiv1.UsageSpec{grant("app")},
			})
			Expect(changes).To(HaveLen(3))
			Expect(changes[0].err).To(MatchError(errConflictingDatabasePermission))
			Expect(changes[1].err).ToNot(HaveOccurred())
		})

		It("folds every spelling of public into the same grantee", func() {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("public"), revoke("PUBLIC")},
			})
			Expect(changes).To(HaveLen(1))
			Expect(changes[0].err).To(MatchError(errConflictingDatabasePermission))
		})

		Context("with revokePublicConnect", func() {
			It("revokes CONNECT from public on its own", func() {
				changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
					RevokePublicConnect: true,
				})
				Expect(changes).To(HaveLen(1))
				Expect(changes[0].privilege).To(Equal("CONNECT"))
				Expect(changes[0].name).To(Equal("PUBLIC"))
				Expect(changes[0].changeType).To(Equal(apiv1.RevokeUsageSpecType))
				Expect(changes[0].err).ToNot(HaveOccurred())
			})

			It("revokes after the other CONNECT entries", func() {
				changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
					RevokePublicConnect: true,
					Connect:             []apiv1.UsageSpec{grant("app")},
					All:                 []apiv1.UsageSpec{grant("reporting")},
				})
				Expect(changes).To(HaveLen(5))
				Expect(changes[0].name).To(Equal("app"))
				Expect(changes[1].name).To(Equal("reporting"))
				Expect(changes[2].name).To(Equal("PUBLIC"))
				Expect(changes[2].privilege).To(Equal("CONNECT"))
			})

			It("leaves public to the user when it is listed under connect", func() {
				changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
					RevokePublicConnect: true,
					Connect:             []apiv1.UsageSpec{grant("Public")},
				})
				Expect(changes).To(HaveLen(1))
				Expect(changes[0].changeType).To(Equal(apiv1.GrantUsageSpecType))
				Expect(changes[0].err).ToNot(HaveOccurred())
			})

			It("leaves public to the user when it is listed under all", func() {
				changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
					RevokePublicConnect: true,
					All:                 []apiv1.UsageSpec{revoke("public")},
				})
				Expect(changes).To(HaveLen(3))
				for _, change := range changes {
					Expect(change.err).ToNot(HaveOccurred())
				}
			})

			It("revokes from public only CONNECT", func() {
				changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
					RevokePublicConnect: true,
					Create:              []apiv1.UsageSpec{grant("app")},
				})
				Expect(changes).To(HaveLen(2))
				Expect(changes[0].privilege).To(Equal("CONNECT"))
				Expect(changes[1].privilege).To(Equal("CREATE"))
			})
		})

		Context("with the metrics exporter", func() {
			It("grants CONNECT to the exporter before the entries of the user", func() {
				changes := expandDatabasePermissions(&apiv1.DatabasePermissionsSpec{
					RevokePublicConnect: true,
					Connect:             []apiv1.UsageSpec{revoke("public"), grant("app")},
				})
				Expect(changes).To(HaveLen(3))
				Expect(changes[0].name).To(Equal(apiv1.MetricsExporterUserName))
				Expect(changes[0].privilege).To(Equal("CONNECT"))
				Expect(changes[0].changeType).To(Equal(apiv1.GrantUsageSpecType))
				Expect(changes[1].name).To(Equal("public"))
			})

			It("only grants CONNECT to the exporter without permissions", func() {
				for _, permissions := range []*apiv1.DatabasePermissionsSpec{nil, {}} {
					changes := expandDatabasePermissions(permissions)
					Expect(changes).To(HaveLen(1))
					Expect(changes[0].name).To(Equal(apiv1.MetricsExporterUserName))
				}
			})
		})
	})

	Describe("planDatabasePermissions", func() {
		// the OID of the roles that exist, as it would be found by decorating
		roles := map[string]int64{"app": 20}

		decorated := func(
			permissions *apiv1.DatabasePermissionsSpec,
			oids map[string]int64,
		) []databasePermissionChange {
			changes := expandUserPermissions(permissions)
			for i := range changes {
				if granteeKey(changes[i].name) != publicRole {
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

		It("treats every spelling of public as PUBLIC", func() {
			for _, name := range []string{"public", "PUBLIC", "Public"} {
				changes := plan(databaseACL{}, &apiv1.DatabasePermissionsSpec{
					Connect: []apiv1.UsageSpec{grant(name)},
				})
				Expect(statements(changes)).To(Equal([]string{
					`GRANT CONNECT ON DATABASE "my-db" TO PUBLIC`,
				}), name)
			}
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
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("ghost")},
			})
			changes[0].err = errDatabasePermissionRoleNotFound

			planned := planDatabasePermissions("my-db", changes, databaseACL{})
			Expect(statements(planned)).To(BeEmpty())
			Expect(planned[0].err).To(MatchError(errDatabasePermissionRoleNotFound))
		})
	})

	Describe("decorateDatabasePermissions", func() {
		var (
			dbMock sqlmock.Sqlmock
			db     *sql.DB
		)

		BeforeEach(func() {
			var err error
			db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		})

		It("sets the OID of the roles, 0 for public without querying it", func(ctx SpecContext) {
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))

			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			Expect(changes[0].granteeOid).To(BeEquivalentTo(20))
			Expect(changes[1].granteeOid).To(BeEquivalentTo(0))
			Expect(changes[0].err).ToNot(HaveOccurred())
			Expect(changes[1].err).ToNot(HaveOccurred())
		})

		It("looks up a role only once, even if it has several privileges", func(ctx SpecContext) {
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))

			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			for _, change := range changes {
				Expect(change.granteeOid).To(BeEquivalentTo(20))
			}
		})

		It("marks the roles that do not exist, for grants and revokes", func(ctx SpecContext) {
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("ghost").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("phantom").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))

			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("ghost"), revoke("phantom")},
			})
			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			Expect(changes[0].err).To(MatchError(errDatabasePermissionRoleNotFound))
			Expect(changes[1].err).To(MatchError(errDatabasePermissionRoleNotFound))
		})

		It("does not look up the roles of the changes that already failed", func(ctx SpecContext) {
			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app")},
			})
			changes[0].err = errRepeatedDatabasePermission

			Expect(decorateDatabasePermissions(ctx, db, changes)).To(Succeed())
			Expect(changes[0].err).To(MatchError(errRepeatedDatabasePermission))
		})

		It("returns the error of a failing lookup", func(ctx SpecContext) {
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").WillReturnError(errors.New("boom"))

			changes := expandUserPermissions(&apiv1.DatabasePermissionsSpec{
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
			dbMock.ExpectQuery(expectedACLQuery).WithArgs("my-db").WillReturnRows(
				sqlmock.NewRows([]string{"grantee", "privilege_type"}).
					AddRow(0, "CONNECT").AddRow(0, "TEMPORARY").
					AddRow(10, "CONNECT").AddRow(10, "CREATE").AddRow(10, "TEMPORARY"))
		}

		expectExporterLookup := func() {
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs(apiv1.MetricsExporterUserName).
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(30))
		}

		expectExporterGrant := func() {
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "cnpg_metrics_exporter"`).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}

		exporterStatus := apiv1.DatabasePermissionStatus{
			Privilege: "CONNECT",
			Name:      apiv1.MetricsExporterUserName,
			Type:      apiv1.GrantUsageSpecType,
			Applied:   true,
		}

		It("only grants CONNECT to the metrics exporter without permissions", func(ctx SpecContext) {
			expectDefaultACL()
			expectExporterLookup()
			expectExporterGrant()

			Expect(reconcile(ctx, nil)).To(Equal([]apiv1.DatabasePermissionStatus{exporterStatus}))
		})

		It("applies the changes and reports every entry", func(ctx SpecContext) {
			expectDefaultACL()
			expectExporterLookup()
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			expectExporterGrant()
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			dbMock.ExpectExec(`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(statuses).To(Equal([]apiv1.DatabasePermissionStatus{
				exporterStatus,
				{Privilege: "CONNECT", Name: "app", Type: apiv1.GrantUsageSpecType, Applied: true},
				{Privilege: "CONNECT", Name: "public", Type: apiv1.RevokeUsageSpecType, Applied: true},
			}))
		})

		It("applies the entries in the order of the specification", func(ctx SpecContext) {
			expectDefaultACL()
			expectExporterLookup()
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			expectExporterGrant()
			dbMock.ExpectExec(`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{revoke("public"), grant("app")},
			})
			Expect(statuses).To(HaveLen(3))
			Expect(statuses[0].Name).To(Equal(apiv1.MetricsExporterUserName))
			Expect(statuses[1].Name).To(Equal("public"))
			Expect(statuses[2].Name).To(Equal("app"))
		})

		It("applies the other entries when a role does not exist", func(ctx SpecContext) {
			expectDefaultACL()
			expectExporterLookup()
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("ghost").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))
			expectExporterGrant()
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), grant("ghost")},
			})
			Expect(statuses).To(HaveLen(3))
			Expect(statuses[1].Applied).To(BeTrue())
			Expect(statuses[2].Applied).To(BeFalse())
			Expect(statuses[2].Message).To(Equal("role does not exist"))
		})

		It("reports a failing statement against its entry only", func(ctx SpecContext) {
			expectDefaultACL()
			expectExporterLookup()
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))
			expectExporterGrant()
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "app"`).
				WillReturnError(errors.New("boom"))
			dbMock.ExpectExec(`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(statuses).To(HaveLen(3))
			Expect(statuses[1].Applied).To(BeFalse())
			Expect(statuses[1].Message).To(Equal("boom"))
			Expect(statuses[2].Applied).To(BeTrue())
		})

		It("reports the failure to read the ACL against every entry", func(ctx SpecContext) {
			dbMock.ExpectQuery(expectedACLQuery).WithArgs("my-db").WillReturnError(errors.New("no connection"))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				All: []apiv1.UsageSpec{grant("app")},
			})
			Expect(statuses).To(HaveLen(4))
			for _, status := range statuses {
				Expect(status.Applied).To(BeFalse())
				Expect(status.Message).To(ContainSubstring("no connection"))
			}
		})

		It("is idempotent: nothing is executed when the ACL matches", func(ctx SpecContext) {
			dbMock.ExpectQuery(expectedACLQuery).WithArgs("my-db").WillReturnRows(
				sqlmock.NewRows([]string{"grantee", "privilege_type"}).AddRow(20, "CONNECT").AddRow(30, "CONNECT"))
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs(apiv1.MetricsExporterUserName).
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(30))
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs("app").
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(20))

			statuses := reconcile(ctx, &apiv1.DatabasePermissionsSpec{
				Connect: []apiv1.UsageSpec{grant("app"), revoke("public")},
			})
			Expect(statuses).To(HaveLen(3))
			for _, status := range statuses {
				Expect(status.Applied).To(BeTrue())
			}
		})
	})

	Describe("applyClusterDatabasePermissions", func() {
		var (
			dbMock sqlmock.Sqlmock
			db     *sql.DB
		)

		BeforeEach(func() {
			var err error
			db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		})

		expectACL := func(rows ...[2]any) {
			result := sqlmock.NewRows([]string{"grantee", "privilege_type"})
			for _, row := range rows {
				result.AddRow(row[0], row[1])
			}
			dbMock.ExpectQuery(expectedACLQuery).WithArgs("my-db").WillReturnRows(result)
		}

		expectExporterLookup := func() {
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs(apiv1.MetricsExporterUserName).
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(30))
		}

		It("grants CONNECT to the exporter, then revokes it from PUBLIC", func(ctx SpecContext) {
			expectACL([2]any{10, "CONNECT"}, [2]any{0, "CONNECT"}, [2]any{0, "TEMPORARY"})
			expectExporterLookup()
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "cnpg_metrics_exporter"`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			dbMock.ExpectExec(`REVOKE CONNECT ON DATABASE "my-db" FROM PUBLIC`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			Expect(applyClusterDatabasePermissions(ctx, db, "my-db", true)).To(Succeed())
		})

		It("leaves PUBLIC alone unless asked to revoke it", func(ctx SpecContext) {
			expectACL([2]any{10, "CONNECT"}, [2]any{0, "CONNECT"})
			expectExporterLookup()
			dbMock.ExpectExec(`GRANT CONNECT ON DATABASE "my-db" TO "cnpg_metrics_exporter"`).
				WillReturnResult(sqlmock.NewResult(0, 0))

			Expect(applyClusterDatabasePermissions(ctx, db, "my-db", false)).To(Succeed())
		})

		It("does nothing when everything is already applied", func(ctx SpecContext) {
			expectACL([2]any{10, "CONNECT"}, [2]any{30, "CONNECT"})
			expectExporterLookup()

			Expect(applyClusterDatabasePermissions(ctx, db, "my-db", true)).To(Succeed())
		})

		It("does nothing when the database doesn't exist", func(ctx SpecContext) {
			expectACL()

			Expect(applyClusterDatabasePermissions(ctx, db, "my-db", true)).To(Succeed())
		})

		It("fails when the exporter role doesn't exist", func(ctx SpecContext) {
			expectACL([2]any{10, "CONNECT"})
			dbMock.ExpectQuery(expectedRoleOidQuery).WithArgs(apiv1.MetricsExporterUserName).
				WillReturnRows(sqlmock.NewRows([]string{"oid"}))

			Expect(applyClusterDatabasePermissions(ctx, db, "my-db", false)).
				To(MatchError(errDatabasePermissionRoleNotFound))
		})
	})
})
