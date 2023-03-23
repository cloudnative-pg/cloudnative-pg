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
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres RoleManager implementation test", func() {
	falseValue := false
	wantedRole := apiv1.RoleConfiguration{
		Name:            "foo",
		BypassRLS:       true,
		CreateDB:        false,
		CreateRole:      true,
		Login:           true,
		Inherit:         &falseValue,
		ConnectionLimit: 2,
		Comment:         "this user is a test",
	}
	unWantedRole := apiv1.RoleConfiguration{
		Name: "foo",
	}
	wantedRoleExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION "+
			"NOSUPERUSER CONNECTION LIMIT 2  PASSWORD NULL",
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
       			pg_catalog.shobj_description(oid, 'pg_authid') as comment, xmin
		FROM pg_catalog.pg_authid where rolname not like 'pg_%'`

	// Testing List
	It("List can read the list of roles from the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		testDate := time.Date(2023, 4, 4, 0, 0, 0, 0, time.UTC)

		rows := sqlmock.NewRows([]string{
			"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
			"rolcanlogin", "rolreplication", "rolconnlimit", "rolpassword", "rolvaliduntil", "rolbypassrls", "comment",
			"xmin",
		}).
			AddRow("postgres", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is postgres user"), 11).
			AddRow("streaming_replica", false, false, true, true, false, true, 10, []byte("54321"),
				testDate, false, []byte("This is streaming_replica user"), 22)
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
		err = prm.Create(ctx, newDatabaseRoleBuilder().withRole(wantedRole).build())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will return error if there is a problem creating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnError(dbError)

		err = prm.Create(ctx, newDatabaseRoleBuilder().withRole(wantedRole).build())
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	// Testing Delete
	It("Delete will send a correct DROP to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Delete(ctx, newDatabaseRoleBuilder().withRole(unWantedRole).build())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Delete will return error if there is a problem deleting the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnError(dbError)

		err = prm.Delete(ctx, newDatabaseRoleBuilder().withRole(unWantedRole).build())
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	// Testing Alter
	It("Update will send a correct ALTER to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))
		err = prm.Update(ctx, newDatabaseRoleBuilder().withRole(wantedRole).build())
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Update will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnError(dbError)

		err = prm.Update(ctx, newDatabaseRoleBuilder().withRole(wantedRole).build())
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
		err = prm.UpdateComment(ctx, newDatabaseRoleBuilder().withRole(wantedRole).build())
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("UpdateComment will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleCommentStmt).
			WillReturnError(dbError)

		err = prm.UpdateComment(ctx, newDatabaseRoleBuilder().withRole(wantedRole).build())
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})
})
