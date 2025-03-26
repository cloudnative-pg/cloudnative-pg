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
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("", func() {
	const inhQuery = "SELECT ur.rolname AS roleid, um.rolname AS member, a.admin_option, ug.rolname AS grantor " +
		"FROM pg_catalog.pg_auth_members a LEFT JOIN pg_catalog.pg_authid ur on ur.oid = a.roleid " +
		"LEFT JOIN pg_catalog.pg_authid um on um.oid = a.member " +
		"LEFT JOIN pg_catalog.pg_authid ug on ug.oid = a.grantor " +
		"WHERE ur.oid >= 16384 AND um.oid >= 16384"

	var (
		fp   fakePooler
		mock sqlmock.Sqlmock
		ri   []RoleInheritance
		rm   roleInheritanceManager
	)

	BeforeEach(func() {
		db, dbMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		mock = dbMock
		fp = fakePooler{
			db: db,
		}
		ri = []RoleInheritance{
			{
				RoleID:      "role1",
				Member:      "member1",
				AdminOption: true,
				Grantor:     func() *string { s := "grantor1"; return &s }(),
			},
		}
		rm = roleInheritanceManager{origin: fp, destination: fp}
	})

	AfterEach(func() {
		expectationErr := mock.ExpectationsWereMet()
		Expect(expectationErr).ToNot(HaveOccurred())
	})

	It("should clone role inheritance successfully", func(ctx SpecContext) {
		// Define the RoleInheritance result for getRoleInheritance
		ri := []RoleInheritance{
			{
				RoleID:      "role1",
				Member:      "member1",
				AdminOption: true,
				Grantor:     func() *string { s := "grantor1"; return &s }(),
			},
		}

		// Define the exact GRANT query
		grantQuery := fmt.Sprintf(`GRANT %s TO %s WITH ADMIN OPTION GRANTED BY %s`,
			pgx.Identifier{ri[0].RoleID}.Sanitize(),
			pgx.Identifier{ri[0].Member}.Sanitize(),
			pgx.Identifier{*ri[0].Grantor}.Sanitize(),
		)

		// Expected queries for getRoleInheritance and importRoleInheritance
		mock.ExpectQuery(inhQuery).
			WillReturnRows(sqlmock.NewRows([]string{"roleid", "member", "admin_option", "grantor"}).
				AddRow(ri[0].RoleID, ri[0].Member, ri[0].AdminOption, *ri[0].Grantor))
		mock.ExpectExec(grantQuery).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := cloneRoleInheritance(ctx, fp, fp)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return any error encountered when getting role inheritance", func(ctx SpecContext) {
		expectedErr := fmt.Errorf("querying error")
		mock.ExpectQuery(inhQuery).WillReturnError(expectedErr)

		err := cloneRoleInheritance(ctx, fp, fp)
		Expect(err).To(Equal(expectedErr))
	})

	It("should import role inheritance successfully", func(ctx SpecContext) {
		query := fmt.Sprintf(`GRANT %s TO %s WITH ADMIN OPTION GRANTED BY %s`,
			pgx.Identifier{ri[0].RoleID}.Sanitize(),
			pgx.Identifier{ri[0].Member}.Sanitize(),
			pgx.Identifier{*ri[0].Grantor}.Sanitize(),
		)

		// Expect a query for each role inheritance
		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(1, 1))

		err := rm.importRoleInheritance(ctx, ri)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return the correct role inheritances", func(ctx SpecContext) {
		mock.ExpectQuery(inhQuery).
			WillReturnRows(sqlmock.NewRows([]string{"roleid", "member", "admin_option", "grantor"}).
				AddRow("role1", "member1", true, "grantor1"))

		ris, err := rm.getRoleInheritance(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(ris).To(Equal(ri))
	})

	It("should return any error encountered when getting role inheritances", func(ctx SpecContext) {
		expectedErr := fmt.Errorf("querying error")
		mock.ExpectQuery(inhQuery).WillReturnError(expectedErr)

		_, err := rm.getRoleInheritance(ctx)
		Expect(err).To(Equal(expectedErr))
	})

	It("should return any error encountered when scanning the result", func(ctx SpecContext) {
		mock.ExpectQuery(inhQuery).WillReturnRows(sqlmock.NewRows([]string{"wrongColumnName"}).AddRow("role1"))

		_, err := rm.getRoleInheritance(ctx)
		Expect(err).To(HaveOccurred())
	})
})
