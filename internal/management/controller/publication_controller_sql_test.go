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

// nolint: dupl
package controller

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("publication sql", func() {
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

	It("drops the publication successfully", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pgx.Identifier{"publication_name"}.Sanitize())).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := executeDropPublication(ctx, db, "publication_name")
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns an error when dropping the publication fails", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP PUBLICATION IF EXISTS %s",
			pgx.Identifier{"publication_name"}.Sanitize())).
			WillReturnError(fmt.Errorf("drop publication error"))

		err := executeDropPublication(ctx, db, "publication_name")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("while dropping publication: drop publication error"))
	})

	It("sanitizes the publication name correctly", func(ctx SpecContext) {
		dbMock.ExpectExec(
			fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pgx.Identifier{"sanitized_name"}.Sanitize())).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := executeDropPublication(ctx, db, "sanitized_name")
		Expect(err).ToNot(HaveOccurred())
	})

	It("generates correct SQL for altering publication with target objects", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{TablesInSchema: "public"},
					},
				},
			},
		}

		sqls := toPublicationAlterSQL(obj)
		Expect(sqls).To(ContainElement(`ALTER PUBLICATION "test_pub" SET TABLES IN SCHEMA "public"`))
	})

	It("generates correct SQL for altering publication with parameters", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Parameters: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
		}

		sqls := toPublicationAlterSQL(obj)
		Expect(sqls).To(ContainElement(`ALTER PUBLICATION "test_pub" SET ("param1" = 'value1', "param2" = 'value2')`))
	})

	It("returns empty SQL list when no alterations are needed", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
			},
		}

		sqls := toPublicationAlterSQL(obj)
		Expect(sqls).To(BeEmpty())
	})

	It("generates correct SQL for creating publication with target schema", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{TablesInSchema: "public"},
					},
				},
			},
		}

		sql := toPublicationCreateSQL(obj)
		Expect(sql).To(Equal(`CREATE PUBLICATION "test_pub" FOR TABLES IN SCHEMA "public"`))
	})

	It("generates correct SQL for creating publication with target table", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{Table: &apiv1.PublicationTargetTable{Name: "table", Schema: "test", Columns: []string{"a", "b"}}},
					},
				},
			},
		}

		sql := toPublicationCreateSQL(obj)
		Expect(sql).To(Equal(`CREATE PUBLICATION "test_pub" FOR TABLE "test"."table" ("a", "b")`))
	})

	It("generates correct SQL for creating publication with parameters", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Parameters: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{TablesInSchema: "public"},
					},
				},
			},
		}

		sql := toPublicationCreateSQL(obj)
		Expect(sql).To(Equal(
			`CREATE PUBLICATION "test_pub" FOR TABLES IN SCHEMA "public" WITH ("param1" = 'value1', "param2" = 'value2')`,
		))
	})
})

var _ = Describe("toPublicationCreateSQL", func() {
	It("returns correct SQL for multiple tables", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{Table: &apiv1.PublicationTargetTable{Name: "crm_employees", Schema: "log_crm"}},
						{Table: &apiv1.PublicationTargetTable{Name: "crm_user_actions", Schema: "log_crm"}},
					},
				},
			},
		}

		sql := toPublicationCreateSQL(obj)
		Expect(sql).To(Equal(
			`CREATE PUBLICATION "test_pub" FOR TABLE "log_crm"."crm_employees", "log_crm"."crm_user_actions"`))
	})

	It("returns correct SQL for mixed tables and schemas (requires pg15+)", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{Table: &apiv1.PublicationTargetTable{Name: "table1"}},
						{Table: &apiv1.PublicationTargetTable{Name: "table2"}},
						{TablesInSchema: "public"},
						{Table: &apiv1.PublicationTargetTable{Name: "table3"}},
					},
				},
			},
		}

		sql := toPublicationCreateSQL(obj)
		Expect(sql).To(Equal(
			`CREATE PUBLICATION "test_pub" FOR TABLE "table1", "table2", TABLES IN SCHEMA "public", TABLE "table3"`))
	})

	It("returns correct SQL for multiple tables with columns", func() {
		obj := &apiv1.Publication{
			Spec: apiv1.PublicationSpec{
				Name: "test_pub",
				Target: apiv1.PublicationTarget{
					Objects: []apiv1.PublicationTargetObject{
						{Table: &apiv1.PublicationTargetTable{Name: "table1", Columns: []string{"a", "b"}}},
						{Table: &apiv1.PublicationTargetTable{Name: "table2", Columns: []string{"c"}}},
					},
				},
			},
		}

		sql := toPublicationCreateSQL(obj)
		Expect(sql).To(Equal(`CREATE PUBLICATION "test_pub" FOR TABLE "table1" ("a", "b"), "table2" ("c")`))
	})
})
