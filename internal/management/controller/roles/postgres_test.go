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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres RoleManager implementation test", func() {
	// Testing List
	It("List can read the list of roles from the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		rows := sqlmock.NewRows([]string{
			"rolname", "rolcreatedb", "rolsuper",
			"rolcanlogin", "rolbypassrls", "rolpassword", "rolvaliduntil",
		}).
			AddRow("postgres", true, false, true, false, []byte("12345"), nil).
			AddRow("streaming_replica", false, true, false, true, []byte("54321"), "2023-04-04")
		mock.ExpectQuery(`SELECT rolname, rolcreatedb, rolsuper, rolcanlogin, rolbypassrls,
		rolpassword, rolvaliduntil
	  FROM pg_catalog.pg_roles where rolname not like 'pg_%';`).WillReturnRows(rows)
		mock.ExpectExec("CREATE ROLE foo").WillReturnResult(sqlmock.NewResult(11, 1))
		roles, err := prm.List(ctx, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(roles).To(HaveLen(2))
		Expect(roles).To(ContainElements(apiv1.RoleConfiguration{
			Name:       "postgres",
			CreateDB:   true,
			Superuser:  false,
			Login:      true,
			BypassRLS:  false,
			ValidUntil: "",
		}))
		Expect(roles).To(ContainElements(apiv1.RoleConfiguration{
			Name:       "streaming_replica",
			CreateDB:   false,
			Superuser:  true,
			Login:      false,
			BypassRLS:  true,
			ValidUntil: "2023-04-04",
		}))
	})
	It("List returns error if there is a problem with the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		dbError := errors.New("Kaboom")
		mock.ExpectQuery(`SELECT rolname, rolcreatedb, rolsuper, rolcanlogin, rolbypassrls,
		rolpassword, rolvaliduntil
	  FROM pg_catalog.pg_roles where rolname not like 'pg_%';`).WillReturnError(dbError)
		roles, err := prm.List(ctx, nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(dbError))
		Expect(roles).To(BeEmpty())
	})
	// Testing Create
	It("Create will send a correct CREATE to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		wantedRole := apiv1.RoleConfiguration{
			Name: "foo",
		}

		mock.ExpectExec(fmt.Sprintf("CREATE ROLE %s", wantedRole.Name)).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Create(ctx, wantedRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Create will return error if there is a problem creating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		wantedRole := apiv1.RoleConfiguration{
			Name: "foo",
		}
		dbError := errors.New("Kaboom")
		mock.ExpectExec(fmt.Sprintf("CREATE ROLE %s", wantedRole.Name)).
			WillReturnError(dbError)

		err = prm.Create(ctx, wantedRole)
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	// Testing Delete
	It("Delete will send a correct DROP to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		unWantedRole := apiv1.RoleConfiguration{
			Name: "foo",
		}

		mock.ExpectExec(fmt.Sprintf("DROP ROLE %s", unWantedRole.Name)).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Delete(ctx, unWantedRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Delete will return error if there is a problem deleting the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		unWantedRole := apiv1.RoleConfiguration{
			Name: "foo",
		}
		dbError := errors.New("Kaboom")
		mock.ExpectExec(fmt.Sprintf("DROP ROLE %s", unWantedRole.Name)).
			WillReturnError(dbError)

		err = prm.Delete(ctx, unWantedRole)
		Expect(err).To(HaveOccurred())
		Expect(errors.Unwrap(err)).To(BeEquivalentTo(dbError))
	})
	// Testing Alter
	It("Update will send a correct ALTER to the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		wantedRole := apiv1.RoleConfiguration{
			Name:      "foo",
			CreateDB:  false,
			BypassRLS: true,
		}

		mock.ExpectExec(
			fmt.Sprintf("ALTER ROLE %s [NOCREATEDB|BYPASSRLS]{2}", wantedRole.Name)).
			WillReturnResult(sqlmock.NewResult(2, 3))

		err = prm.Update(ctx, wantedRole)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("Update will return error if there is a problem updating the role in the DB", func(ctx context.Context) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())
		prm := NewPostgresRoleManager(db)

		wantedRole := apiv1.RoleConfiguration{
			Name:      "foo",
			CreateDB:  false,
			BypassRLS: true,
		}
		dbError := errors.New("Kaboom")
		mock.ExpectExec(
			fmt.Sprintf("ALTER ROLE %s [NOCREATEDB|BYPASSRLS]{2}", wantedRole.Name)).
			WillReturnError(dbError)

		err = prm.Update(ctx, wantedRole)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(dbError))
	})
})
