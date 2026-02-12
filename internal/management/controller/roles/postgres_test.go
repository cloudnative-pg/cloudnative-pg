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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres RoleManager implementation test", func() {
	falseValue := false
	validUntil := metav1.Date(2100, 0o1, 0o1, 0o0, 0o0, 0o0, 0o0, time.UTC)
	wantedRole := apiv1.RoleConfiguration{
		Name:            "foo",
		BypassRLS:       true,
		CreateDB:        false,
		CreateRole:      true,
		Login:           true,
		Inherit:         &falseValue,
		ConnectionLimit: 2,
		Comment:         "this user is a test",
		ValidUntil:      &validUntil,
		InRoles:         []string{"pg_monitoring"},
	}
	internalWantedRole := roleConfigurationAdapter{RoleConfiguration: wantedRole}

	wantedRoleWithPass := apiv1.RoleConfiguration{
		Name:            "foo",
		BypassRLS:       true,
		CreateDB:        false,
		CreateRole:      true,
		Login:           true,
		Inherit:         &falseValue,
		ConnectionLimit: 2,
		Comment:         "this user is a test",
		ValidUntil:      &validUntil,
		InRoles:         []string{"pg_monitoring"},
		PasswordSecret: &apiv1.LocalObjectReference{
			Name: "mySecret",
		},
	}
	wantedRoleWithoutValidUntil := apiv1.RoleConfiguration{
		Name:            "foo",
		BypassRLS:       true,
		CreateDB:        false,
		CreateRole:      true,
		Login:           true,
		Inherit:         &falseValue,
		ConnectionLimit: 2,
		Comment:         "this user is a test",
		InRoles:         []string{"pg_monitoring"},
		PasswordSecret: &apiv1.LocalObjectReference{
			Name: "mySecret",
		},
	}
	wantedRoleWithPassDeletion := apiv1.RoleConfiguration{
		Name:            "foo",
		BypassRLS:       true,
		CreateDB:        false,
		CreateRole:      true,
		Login:           true,
		Inherit:         &falseValue,
		ConnectionLimit: 2,
		Comment:         "this user is a test",
		ValidUntil:      &validUntil,
		InRoles:         []string{"pg_monitoring"},
		DisablePassword: true,
	}
	wantedRoleWithDefaultConnectionLimit := apiv1.RoleConfiguration{
		Name:            "foo",
		ConnectionLimit: -1,
	}
	unWantedRole := apiv1.RoleConfiguration{
		Name: "foo",
	}
	wantedRoleExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE \"pg_monitoring\" VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)

	wantedRoleWithPassExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE \"pg_monitoring\" PASSWORD 'myPassword' VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)

	wantedLogPreventionStmt := "SET LOCAL log_min_error_statement = 'PANIC'"

	wantedRoleWithoutValidUntilExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE \"pg_monitoring\" PASSWORD 'myPassword'",
		wantedRole.Name)

	wantedRoleWithPassDeletionExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE \"pg_monitoring\" PASSWORD NULL VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)
	wantedRoleWithDefaultConnectionLimitExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" NOBYPASSRLS NOCREATEDB NOCREATEROLE INHERIT NOLOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT -1",
		wantedRoleWithDefaultConnectionLimit.Name)

	wantedRoleCommentStmt := fmt.Sprintf(
		wantedRoleCommentTpl,
		wantedRole.Name, pq.QuoteLiteral(wantedRole.Comment))

	wantedRoleExpectedAltStmt := fmt.Sprintf(
		"ALTER ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 2 "+
			"VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)
	wantedRoleExpectedAltWithPasswordStmt := fmt.Sprintf(
		"ALTER ROLE \"%s\"  BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 2 "+
			"PASSWORD 'myPassword' VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)
	unWantedRoleExpectedDelStmt := fmt.Sprintf("DROP ROLE \"%s\"", unWantedRole.Name)

	// Testing List
	It("List can read the list of roles from the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		testDate := time.Date(2023, 4, 4, 0, 0, 0, 0, time.UTC)

		rows := sqlmock.NewRows([]string{
			"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
			"rolcanlogin", "rolreplication", "rolconnlimit", "rolpassword", "rolvaliduntil", "rolbypassrls", "comment",
			"xmin", "inroles",
		}).
			AddRow("postgres", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is postgres user"), 11, []byte("{}")).
			AddRow("streaming_replica", false, false, true, true, false, true, 10, []byte("54321"),
				pgtype.Timestamp{
					Valid:            true,
					Time:             testDate,
					InfinityModifier: pgtype.Finite,
				}, false, []byte("This is streaming_replica user"), 22, []byte(`{"role1","role2"}`)).
			AddRow("future_man", false, false, true, true, false, true, 10, []byte("54321"),
				pgtype.Timestamp{
					Valid:            true,
					Time:             time.Time{},
					InfinityModifier: pgtype.Infinity,
				}, false, []byte("This is streaming_replica user"), 22, []byte(`{"role1","role2"}`))
		mock.ExpectQuery(expectedSelStmt).WillReturnRows(rows)
		mock.ExpectExec("CREATE ROLE foo").WillReturnResult(sqlmock.NewResult(11, 1))
		roles, err := List(ctx, db)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(roles).To(HaveLen(3))
		password1 := sql.NullString{
			Valid:  true,
			String: "12345",
		}
		password2 := sql.NullString{
			Valid:  true,
			String: "54321",
		}
		Expect(roles).To(ContainElements(DatabaseRole{
			Name:            "postgres",
			CreateDB:        true,
			CreateRole:      true,
			Superuser:       true,
			Inherit:         false,
			Login:           true,
			Replication:     false,
			BypassRLS:       false,
			ConnectionLimit: -1,
			ValidUntil:      pgtype.Timestamp{},
			Comment:         "This is postgres user",
			password:        password1,
			transactionID:   11,
			InRoles:         []string{},
		}, DatabaseRole{
			Name:            "streaming_replica",
			CreateDB:        true,
			CreateRole:      true,
			Superuser:       false,
			Inherit:         false,
			Login:           false,
			Replication:     true,
			BypassRLS:       false,
			ConnectionLimit: 10,
			ValidUntil:      pgtype.Timestamp{Valid: true, Time: testDate},
			Comment:         "This is streaming_replica user",
			password:        password2,
			transactionID:   22,
			InRoles: []string{
				"role1",
				"role2",
			},
		}))
	})
	It("List returns error if there is a problem with the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		dbError := errors.New("Kaboom")
		mock.ExpectQuery(expectedSelStmt).WillReturnError(dbError)
		roles, err := List(ctx, db)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(BeEquivalentTo("while listing DB roles for role reconciler: Kaboom"))
		Expect(roles).To(BeEmpty())
	})
	// Testing Create
	It("Create will send a correct CREATE to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = Create(ctx, db, internalWantedRole.toDatabaseRole())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will return error if there is a problem creating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnError(dbError)

		err = Create(ctx, db, internalWantedRole.toDatabaseRole())
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	It("Create with password will send CREATE and prevent Postgres from logging the query", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectBegin()
		mock.ExpectExec(wantedLogPreventionStmt).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(wantedRoleWithPassExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		mock.ExpectCommit()

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		dbRole := roleConfigurationAdapter{RoleConfiguration: wantedRoleWithPass}.toDatabaseRole()
		// In this unit test we are not testing the retrieval of secrets, so let's
		// fetch the password content by hand
		dbRole.password = sql.NullString{Valid: true, String: "myPassword"}
		err = Create(ctx, db, dbRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will send a correct CREATE with perpetual password to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectBegin()
		mock.ExpectExec(wantedLogPreventionStmt).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(wantedRoleWithoutValidUntilExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		mock.ExpectCommit()

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		dbRole := roleConfigurationAdapter{
			RoleConfiguration: wantedRoleWithoutValidUntil,
		}.toDatabaseRole()
		// In this unit test we are not testing the retrieval of secrets, so let's
		// fetch the password content by hand
		dbRole.password = sql.NullString{Valid: true, String: "myPassword"}
		err = Create(ctx, db, dbRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create with password will rollback CREATE if there is an error", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectBegin()
		mock.ExpectExec(wantedLogPreventionStmt).
			WillReturnResult(sqlmock.NewResult(0, 1))
		dbError := errors.New("kaboom")
		mock.ExpectExec(wantedRoleWithPassExpectedCrtStmt).
			WillReturnError(dbError)
		mock.ExpectRollback()

		dbRole := roleConfigurationAdapter{RoleConfiguration: wantedRoleWithPass}.toDatabaseRole()
		// In this unit test we are not testing the retrieval of secrets, so let's
		// fetch the password content by hand
		dbRole.password = sql.NullString{Valid: true, String: "myPassword"}
		err = Create(ctx, db, dbRole)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})

	It("Create will send a correct CREATE with password deletion to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectExec(wantedRoleWithPassDeletionExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = Create(ctx, db,
			roleConfigurationAdapter{RoleConfiguration: wantedRoleWithPassDeletion}.toDatabaseRole())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will send a correct CREATE with password deletion to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectExec(wantedRoleWithDefaultConnectionLimitExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = Create(ctx, db,
			roleConfigurationAdapter{RoleConfiguration: wantedRoleWithDefaultConnectionLimit}.toDatabaseRole())
		Expect(err).ShouldNot(HaveOccurred())
	})
	// Testing Delete
	It("Delete will send a correct DROP to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = Delete(ctx, db, roleConfigurationAdapter{RoleConfiguration: unWantedRole}.toDatabaseRole())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Delete will return error if there is a problem deleting the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		dbError := errors.New("Kaboom")
		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnError(dbError)

		err = Delete(ctx, db, roleConfigurationAdapter{RoleConfiguration: unWantedRole}.toDatabaseRole())
		Expect(err).To(HaveOccurred())
		coreErr := errors.Unwrap(err)
		Expect(coreErr).To(BeEquivalentTo(dbError))
	})
	// Testing Alter
	It("Update will send a correct ALTER to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = Update(ctx, db, roleConfigurationAdapter{RoleConfiguration: wantedRole}.toDatabaseRole())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Update with password will ALTER the DB and prevent Postgres from logging the query", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectBegin()
		mock.ExpectExec(wantedLogPreventionStmt).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(wantedRoleExpectedAltWithPasswordStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		mock.ExpectCommit()

		dbRole := roleConfigurationAdapter{RoleConfiguration: wantedRoleWithPass}.toDatabaseRole()
		dbRole.password = sql.NullString{Valid: true, String: "myPassword"}
		err = Update(ctx, db, dbRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Update with password will rollback ALTER if there is an error", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectBegin()
		mock.ExpectExec(wantedLogPreventionStmt).
			WillReturnResult(sqlmock.NewResult(0, 1))
		dbError := errors.New("kaboom")
		mock.ExpectExec(wantedRoleExpectedAltWithPasswordStmt).
			WillReturnError(dbError)
		mock.ExpectRollback()

		dbRole := roleConfigurationAdapter{RoleConfiguration: wantedRoleWithPass}.toDatabaseRole()
		dbRole.password = sql.NullString{Valid: true, String: "myPassword"}
		err = Update(ctx, db, dbRole)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})
	It("Update will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnError(dbError)

		err = Update(ctx, db, roleConfigurationAdapter{RoleConfiguration: wantedRole}.toDatabaseRole())
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})

	// Testing COMMENT
	It("UpdateComment will send a correct COMMENT to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = UpdateComment(ctx, db, roleConfigurationAdapter{RoleConfiguration: wantedRole}.toDatabaseRole())
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("UpdateComment will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnError(dbError)

		err = UpdateComment(ctx, db, roleConfigurationAdapter{RoleConfiguration: wantedRole}.toDatabaseRole())
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})

	It("GetParentRoles will return the roles a given role belongs to", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := sqlmock.NewRows([]string{
			"inroles",
		}).
			AddRow([]byte(`{"role1","role2"}`))
		mock.ExpectQuery(expectedMembershipStmt).WithArgs("foo").WillReturnRows(rows)

		roles, err := GetParentRoles(ctx, db, DatabaseRole{Name: "foo"})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(roles).To(HaveLen(2))
		Expect(roles).To(ConsistOf("role1", "role2"))
	})

	It("GetParentRoles will error if there is a problem querying the database", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectQuery(expectedMembershipStmt).WithArgs("foo").WillReturnError(fmt.Errorf("kaboom"))
		roles, err := GetParentRoles(ctx, db, DatabaseRole{Name: "foo"})
		Expect(err).Should(HaveOccurred())
		Expect(roles).To(BeEmpty())
	})

	It("UpdateMembership will send correct GRANT and REVOKE statements to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		expectedMembershipExecs := []string{
			`GRANT "pg_monitor" TO "foo"`,
			`GRANT "quux" TO "foo"`,
			`REVOKE "bar" FROM "foo"`,
		}

		mock.ExpectBegin()

		for _, ex := range expectedMembershipExecs {
			mock.ExpectExec(ex).
				WillReturnResult(sqlmock.NewResult(2, 3))
		}

		mock.ExpectCommit()

		err = UpdateMembership(ctx, db, DatabaseRole{Name: "foo"}, []string{"pg_monitor", "quux"}, []string{"bar"})
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("UpdateMembership will roll back if there is an error in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		okMembership := `GRANT "pg_monitor" TO "foo"`
		badMembership := `GRANT "quux" TO "foo"`

		mock.ExpectBegin()

		mock.ExpectExec(okMembership).
			WillReturnResult(sqlmock.NewResult(2, 3))
		mock.ExpectExec(badMembership).WillReturnError(fmt.Errorf("kaboom"))

		mock.ExpectRollback()

		err = UpdateMembership(ctx, db, DatabaseRole{Name: "foo"}, []string{"pg_monitor", "quux"}, []string{"bar"})
		Expect(err).Should(HaveOccurred())
	})

	It("All the roles are false", func() {
		roleWithNo := DatabaseRole{
			BypassRLS:       false,
			CreateDB:        false,
			CreateRole:      false,
			Inherit:         false,
			Login:           false,
			Replication:     false,
			Superuser:       false,
			ConnectionLimit: 0,
		}
		var query strings.Builder
		query.WriteString(fmt.Sprintf("ALTER ROLE %s", pgx.Identifier{"alighieri"}.Sanitize()))
		appendRoleOptions(roleWithNo, &query)

		expectedQuery := "ALTER ROLE \"alighieri\" NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT NOLOGIN " +
			"NOREPLICATION NOSUPERUSER CONNECTION LIMIT 0"
		Expect(query.String()).To(BeEquivalentTo(expectedQuery))
	})

	It("All the roles are true", func() {
		roles := DatabaseRole{
			BypassRLS:       true,
			CreateDB:        true,
			CreateRole:      true,
			Inherit:         true,
			Login:           true,
			Replication:     true,
			Superuser:       true,
			ConnectionLimit: 10,
		}
		var query strings.Builder
		expectedQuery := "ALTER ROLE \"alighieri\" BYPASSRLS CREATEDB CREATEROLE INHERIT LOGIN " +
			"REPLICATION SUPERUSER CONNECTION LIMIT 10"

		query.WriteString(fmt.Sprintf("ALTER ROLE %s", pgx.Identifier{"alighieri"}.Sanitize()))
		appendRoleOptions(roles, &query)
		Expect(query.String()).To(BeEquivalentTo(expectedQuery))
	})

	It("Password with null and with valid until password", func() {
		role := apiv1.RoleConfiguration{}
		dbRole := roleConfigurationAdapter{RoleConfiguration: role}.toDatabaseRole()
		dbRole.password = sql.NullString{Valid: true, String: "divine comedy"}
		dbRole.ignorePassword = false
		Expect(dbRole.password.Valid).To(BeTrue())

		var query strings.Builder
		expectedQuery := "ALTER ROLE \"alighieri\" PASSWORD 'divine comedy'"

		query.WriteString(fmt.Sprintf("ALTER ROLE %s", pgx.Identifier{"alighieri"}.Sanitize()))
		appendPasswordOption(dbRole, &query)
		Expect(query.String()).To(BeEquivalentTo(expectedQuery))
	})

	It("password with valid until", func() {
		role := apiv1.RoleConfiguration{}
		var queryValidUntil strings.Builder
		queryValidUntil.WriteString(fmt.Sprintf("ALTER ROLE %s", pgx.Identifier{"alighieri"}.Sanitize()))
		expectedQueryValidUntil := "ALTER ROLE \"alighieri\" PASSWORD 'divine comedy' VALID UNTIL '2100-01-01 01:01:00Z'"
		validUntil := metav1.Date(2100, 0o1, 0o1, 0o1, 0o1, 0o0, 0o0, time.UTC)
		role.ValidUntil = &validUntil

		dbRole := roleConfigurationAdapter{RoleConfiguration: role}.toDatabaseRole()
		dbRole.password = sql.NullString{Valid: true, String: "divine comedy"}
		dbRole.ignorePassword = false
		appendPasswordOption(dbRole, &queryValidUntil)
		Expect(queryValidUntil.String()).To(BeEquivalentTo(expectedQueryValidUntil))
	})

	It("Getting the proper TransactionID per rol", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := mock.NewRows([]string{"xmin"})
		lastTransactionQuery := "SELECT xmin FROM pg_catalog.pg_authid WHERE rolname = $1"
		dbRole := roleConfigurationAdapter{RoleConfiguration: wantedRole}.toDatabaseRole()

		mock.ExpectQuery(lastTransactionQuery).WithArgs("foo").WillReturnError(errors.New("Kaboom"))
		_, err = GetLastTransactionID(ctx, db, dbRole)
		Expect(err).To(HaveOccurred())

		mock.ExpectQuery(lastTransactionQuery).WithArgs("foo").WillReturnError(sql.ErrNoRows)
		_, err = GetLastTransactionID(ctx, db, dbRole)
		Expect(err).To(HaveOccurred())

		rows.AddRow("1321")
		mock.ExpectQuery(lastTransactionQuery).WithArgs("foo").WillReturnRows(rows)
		transID, err := GetLastTransactionID(ctx, db, dbRole)
		Expect(err).ToNot(HaveOccurred())
		Expect(transID).To(BeEquivalentTo(1321))
	})
})
