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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
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
	unWantedRole := apiv1.RoleConfiguration{
		Name: "foo",
	}
	wantedRoleExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE pg_monitoring VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)

	wantedRoleWithPassExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE pg_monitoring PASSWORD 'myPassword' VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)

	wantedRoleWithPassDeletionExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2 IN ROLE pg_monitoring PASSWORD NULL VALID UNTIL '2100-01-01 00:00:00Z'",
		wantedRole.Name)

	wantedRoleCommentStmt := fmt.Sprintf(
		"COMMENT ON ROLE \"%s\" IS %s",
		wantedRole.Name, pq.QuoteLiteral(wantedRole.Comment))

	wantedRoleExpectedAltStmt := fmt.Sprintf(
		"ALTER ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 2 ",
		wantedRole.Name)
	unWantedRoleExpectedDelStmt := fmt.Sprintf("DROP ROLE \"%s\"", unWantedRole.Name)
	expectedSelStmt := `SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, 
       			rolcanlogin, rolreplication, rolconnlimit, rolpassword, rolvaliduntil, rolbypassrls,
				pg_catalog.shobj_description(auth.oid, 'pg_authid') as comment, auth.xmin,
				mem.inroles
		FROM pg_catalog.pg_authid as auth
		LEFT JOIN (
			SELECT array_agg(pg_get_userbyid(roleid)) as inroles, member
			FROM pg_auth_members GROUP BY member
		) mem ON member = oid
		WHERE rolname not like 'pg_%'`

	expectedMembershipStmt := `SELECT mem.inroles 
		FROM pg_catalog.pg_authid as auth
		LEFT JOIN (
			SELECT array_agg(pg_get_userbyid(roleid)) as inroles, member
			FROM pg_auth_members GROUP BY member
		) mem ON member = oid
		WHERE rolname = $1`

	// Testing List
	It("List can read the list of roles from the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		testDate := time.Date(2023, 4, 4, 0, 0, 0, 0, time.UTC)

		rows := sqlmock.NewRows([]string{
			"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
			"rolcanlogin", "rolreplication", "rolconnlimit", "rolpassword", "rolvaliduntil", "rolbypassrls", "comment",
			"xmin", "inroles",
		}).
			AddRow("postgres", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is postgres user"), 11, []byte("{}")).
			AddRow("streaming_replica", false, false, true, true, false, true, 10, []byte("54321"),
				testDate, false, []byte("This is streaming_replica user"), 22, []byte(`{"role1","role2"}`))
		mock.ExpectQuery(expectedSelStmt).WillReturnRows(rows)
		mock.ExpectExec("CREATE ROLE foo").WillReturnResult(sqlmock.NewResult(11, 1))
		roles, err := prm.List(ctx)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(roles).To(HaveLen(2))
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
			ValidUntil:      nil,
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
			ValidUntil:      &testDate,
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
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectQuery(expectedSelStmt).WillReturnError(dbError)
		roles, err := prm.List(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(dbError))
		Expect(roles).To(BeEmpty())
	})
	// Testing Create
	It("Create will send a correct CREATE to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = prm.Create(ctx, roleFromSpec(wantedRole))
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will return error if there is a problem creating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnError(dbError)

		err = prm.Create(ctx, roleFromSpec(wantedRole))
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	It("Create will send a correct CREATE with password to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(wantedRoleWithPassExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		dbRole := roleFromSpec(wantedRoleWithPass)
		// In this unit test we are not testing the retrieval of secrets, so let's
		// fetch the password content by hand
		dbRole.password = sql.NullString{Valid: true, String: "myPassword"}
		err = prm.Create(ctx, dbRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will send a correct CREATE with password deletion to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(wantedRoleWithPassDeletionExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = prm.Create(ctx, roleFromSpec(wantedRoleWithPassDeletion))
		Expect(err).ShouldNot(HaveOccurred())
	})
	// Testing Delete
	It("Delete will send a correct DROP to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Delete(ctx, roleFromSpec(unWantedRole))
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Delete will return error if there is a problem deleting the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnError(dbError)

		err = prm.Delete(ctx, roleFromSpec(unWantedRole))
		Expect(err).To(HaveOccurred())
		coreErr := errors.Unwrap(err)
		Expect(coreErr).To(BeEquivalentTo(dbError))
	})
	// Testing Alter
	It("Update will send a correct ALTER to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = prm.Update(ctx, roleFromSpec(wantedRole))
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Update will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnError(dbError)

		err = prm.Update(ctx, roleFromSpec(wantedRole))
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})

	// Testing COMMENT
	It("UpdateComment will send a correct COMMENT to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = prm.UpdateComment(ctx, roleFromSpec(wantedRole))
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("UpdateComment will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnError(dbError)

		err = prm.UpdateComment(ctx, roleFromSpec(wantedRole))
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})

	It("GetParentRoles will return the roles a given role belongs to", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		rows := sqlmock.NewRows([]string{
			"inroles",
		}).
			AddRow([]byte(`{"role1","role2"}`))
		mock.ExpectQuery(expectedMembershipStmt).WillReturnRows(rows)

		roles, err := prm.GetParentRoles(ctx, DatabaseRole{Name: "foo"})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(roles).To(HaveLen(2))
		Expect(roles).To(ConsistOf("role1", "role2"))
	})

	It("GetParentRoles will error if there is a problem querying the database", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectQuery(expectedMembershipStmt).WillReturnError(fmt.Errorf("kaboom"))
		roles, err := prm.GetParentRoles(ctx, DatabaseRole{Name: "foo"})
		Expect(err).Should(HaveOccurred())
		Expect(roles).To(HaveLen(0))
	})

	It("UpdateMembership will send correct GRANT and REVOKE statements to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

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

		err = prm.UpdateMembership(ctx, DatabaseRole{Name: "foo"}, []string{"pg_monitor", "quux"}, []string{"bar"})
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("UpdateMembership will roll back if there is an error in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		okMembership := `GRANT "pg_monitor" TO "foo"`
		badMembership := `GRANT "quux" TO "foo"`

		mock.ExpectBegin()

		mock.ExpectExec(okMembership).
			WillReturnResult(sqlmock.NewResult(2, 3))
		mock.ExpectExec(badMembership).WillReturnError(fmt.Errorf("kaboom"))

		mock.ExpectRollback()

		err = prm.UpdateMembership(ctx, DatabaseRole{Name: "foo"}, []string{"pg_monitor", "quux"}, []string{"bar"})
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
		dbRole := roleFromSpec(role)
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

		dbRole := roleFromSpec(role)
		dbRole.password = sql.NullString{Valid: true, String: "divine comedy"}
		dbRole.ignorePassword = false
		appendPasswordOption(dbRole, &queryValidUntil)
		Expect(queryValidUntil.String()).To(BeEquivalentTo(expectedQueryValidUntil))
	})

	It("Getting the proper TransactionID per rol", func() {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		rows := mock.NewRows([]string{"xmin"})
		lastTransactionQuery := "SELECT xmin FROM pg_catalog.pg_authid WHERE rolname = $1"
		dbRole := roleFromSpec(wantedRole)

		mock.ExpectQuery(lastTransactionQuery).WillReturnError(errors.New("Kaboom"))
		_, err = prm.GetLastTransactionID(context.TODO(), dbRole)
		Expect(err).To(HaveOccurred())

		mock.ExpectQuery(lastTransactionQuery).WillReturnError(sql.ErrNoRows)
		_, err = prm.GetLastTransactionID(context.TODO(), dbRole)
		Expect(err).To(HaveOccurred())

		rows.AddRow("1321")
		mock.ExpectQuery(lastTransactionQuery).WillReturnRows(rows)
		transID, err := prm.GetLastTransactionID(context.TODO(), dbRole)
		Expect(err).ToNot(HaveOccurred())
		Expect(transID).To(BeEquivalentTo(1321))
	})
})
