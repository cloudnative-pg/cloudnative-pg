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
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RoleInheritanceManager", func() {
	var (
		mock    sqlmock.Sqlmock
		db      *sql.DB
		ctx     context.Context
		fp      pool.Pooler
		manager roleInheritanceManager
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).ShouldNot(HaveOccurred())

		ctx = context.Background()
		fp = &fakePooler{db: db}

		manager = roleInheritanceManager{
			origin:      fp,
			destination: fp,
		}
	})

	AfterEach(func() {
		expectationErr := mock.ExpectationsWereMet()
		Expect(expectationErr).ToNot(HaveOccurred())
	})

	Context("GetRoleInheritance", func() {
		It("should fetch role inheritance successfully", func() {
			rows := sqlmock.NewRows([]string{"roleid", "member", "admin_option", "grantor"}).
				AddRow("role1", "member1", true, "grantor1").
				AddRow("role2", "member2", false, nil)

			query := "SELECT ur\\.rolname AS roleid, um\\.rolname AS member, a\\.admin_option, ug\\.rolname AS grantor " +
				"FROM pg_catalog.pg_auth_members a LEFT JOIN pg_catalog.pg_authid ur on ur\\.oid = a\\.roleid " +
				"LEFT JOIN pg_catalog.pg_authid um on um\\.oid = a\\.member " +
				"LEFT JOIN pg_catalog.pg_authid ug on ug\\.oid = a\\.grantor " +
				"WHERE ur\\.oid >= 16384 AND um\\.oid >= 16384"

			mock.ExpectQuery(query).WillReturnRows(rows)

			result, err := manager.getRoleInheritance(ctx)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(HaveLen(2))
			Expect(result[0].RoleID).To(Equal("role1"))
			Expect(result[0].Member).To(Equal("member1"))
			Expect(result[0].AdminOption).To(BeTrue())
			Expect(*result[0].Grantor).To(Equal("grantor1"))
			Expect(result[1].RoleID).To(Equal("role2"))
			Expect(result[1].Member).To(Equal("member2"))
			Expect(result[1].AdminOption).To(BeFalse())
			Expect(result[1].Grantor).To(BeNil())
		})
	})
	Context("ImportRoleInheritance", func() {
		It("should import role inheritance successfully", func() {
			ris := []RoleInheritance{
				{
					RoleID:      "role1",
					Member:      "member1",
					AdminOption: true,
					Grantor:     ptr.To("grantor1"),
				},
				{
					RoleID:      "role2",
					Member:      "member2",
					AdminOption: false,
				},
			}

			for _, ri := range ris {
				grantQuery := fmt.Sprintf(
					"GRANT %s TO %s ",
					pgx.Identifier{ri.RoleID}.Sanitize(),
					pgx.Identifier{ri.Member}.Sanitize(),
				)
				if ri.AdminOption {
					grantQuery += "WITH ADMIN OPTION "
				}
				if ri.Grantor != nil {
					grantQuery += fmt.Sprintf("GRANTED BY %s", pgx.Identifier{*ri.Grantor}.Sanitize())
				}
				mock.ExpectExec(grantQuery).WillReturnResult(sqlmock.NewResult(1, 1))
			}

			err := manager.importRoleInheritance(ctx, ris)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
