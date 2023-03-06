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
	"errors"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	controllerScheme "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres RoleManager implementation test", func() {
	client := generateFakeClient()
	instance := postgres.Instance{
		Namespace: "fakeNS",
	}

	wantedRole := apiv1.RoleConfiguration{
		Name:            "foo",
		BypassRLS:       true,
		CreateDB:        false,
		CreateRole:      true,
		Login:           true,
		Inherit:         false,
		ConnectionLimit: 2,
	}
	unWantedRole := apiv1.RoleConfiguration{
		Name: "foo",
	}
	wantedRoleExpectedCrtStmt := fmt.Sprintf(
		"CREATE ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 2 ",
		wantedRole.Name)

	wantedRoleExpectedAltStmt := fmt.Sprintf(
		"ALTER ROLE \"%s\" BYPASSRLS NOCREATEDB CREATEROLE NOINHERIT LOGIN NOREPLICATION NOSUPERUSER CONNECTION LIMIT 2 ",
		wantedRole.Name)
	unWantedRoleExpectedDelStmt := fmt.Sprintf("DROP ROLE \"%s\"", unWantedRole.Name)
	expectedSelStmt := `SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, 
       			rolcanlogin, rolreplication, rolconnlimit, rolpassword, rolvaliduntil, rolbypassrls,
       			pg_catalog.shobj_description(oid, 'pg_authid') as comment
		FROM pg_catalog.pg_authid where rolname not like 'pg_%';`

	// Testing List
	It("List can read the list of roles from the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		rows := sqlmock.NewRows([]string{
			"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
			"rolcanlogin", "rolreplication", "rolconnlimit", "rolpassword", "rolvaliduntil", "rolbypassrls", "comment",
		}).
			AddRow("postgres", true, false, true, true, true, false, -1, []byte("12345"),
				nil, false, []byte("This is postgres user")).
			AddRow("streaming_replica", false, false, true, true, false, true, 10, []byte("54321"),
				"2023-04-04", false, []byte("This is streaming_replica user"))
		mock.ExpectQuery(expectedSelStmt).WillReturnRows(rows)
		mock.ExpectExec("CREATE ROLE foo").WillReturnResult(sqlmock.NewResult(11, 1))
		roles, err := prm.List(ctx, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(roles).To(HaveLen(2))
		password1 := "12345"
		password2 := "54321"
		Expect(roles).To(ContainElements(apiv1.RoleConfiguration{
			Name:            "postgres",
			CreateDB:        true,
			CreateRole:      true,
			Superuser:       true,
			Inherit:         false,
			Login:           true,
			Replication:     false,
			BypassRLS:       false,
			Password:        &password1,
			PasswordSecret:  nil,
			ConnectionLimit: -1,
			ValidUntil:      "",
			Comment:         "This is postgres user",
		}))
		Expect(roles).To(ContainElements(apiv1.RoleConfiguration{
			Name:            "streaming_replica",
			CreateDB:        true,
			CreateRole:      true,
			Superuser:       false,
			Inherit:         false,
			Login:           false,
			Replication:     true,
			BypassRLS:       false,
			Password:        &password2,
			PasswordSecret:  nil,
			ConnectionLimit: 10,
			ValidUntil:      "2023-04-04",
			Comment:         "This is streaming_replica user",
		}))
	})
	It("List returns error if there is a problem with the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		dbError := errors.New("Kaboom")
		mock.ExpectQuery(expectedSelStmt).WillReturnError(dbError)
		roles, err := prm.List(ctx, nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(dbError))
		Expect(roles).To(BeEmpty())
	})
	// Testing Create
	It("Create will send a correct CREATE to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Create(ctx, wantedRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will return error if there is a problem creating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedCrtStmt).
			WillReturnError(dbError)

		err = prm.Create(ctx, wantedRole)
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	// Testing Delete
	It("Delete will send a correct DROP to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Delete(ctx, unWantedRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Delete will return error if there is a problem deleting the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(unWantedRoleExpectedDelStmt).
			WillReturnError(dbError)

		err = prm.Delete(ctx, unWantedRole)
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	// Testing Alter
	It("Update will send a correct ALTER to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Update(ctx, wantedRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Update will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db, client, &instance)

		dbError := errors.New("Kaboom")
		mock.ExpectExec(wantedRoleExpectedAltStmt).
			WillReturnError(dbError)

		err = prm.Update(ctx, wantedRole)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(dbError))
	})
})

func generateFakeClient() client.Client {
	scheme := controllerScheme.BuildWithAllKnownScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
}
