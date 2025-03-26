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

package infrastructure

import (
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres tablespaces functions test", func() {
	expectedListStmt := `
	SELECT
		pg_tablespace.spcname spcname,
		COALESCE(pg_roles.rolname, '') rolname
	FROM pg_catalog.pg_tablespace
	LEFT JOIN pg_catalog.pg_roles ON pg_tablespace.spcowner = pg_roles.oid
	WHERE spcname NOT LIKE $1
	`
	expectedCreateStmt := "CREATE TABLESPACE \"%s\" OWNER \"%s\" " +
		"LOCATION '/var/lib/postgresql/tablespaces/atablespace/data'"

	expectedUpdateStmt := "ALTER TABLESPACE \"%s\" OWNER TO \"%s\""

	It("should send the expected query to list tablespaces and parse the return", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		tbsName := "atablespace"
		anotherTbsName := "anothertablespace"
		ownerName := "postgres"

		rows := sqlmock.NewRows(
			[]string{"spcname", "rolname"}).
			AddRow(tbsName, ownerName).
			AddRow(anotherTbsName, ownerName)
		mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnRows(rows)
		tbs, err := List(ctx, db)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(tbs).To(HaveLen(2))
		Expect(tbs).To(ConsistOf(
			Tablespace{Name: tbsName, Owner: ownerName},
			Tablespace{Name: anotherTbsName, Owner: ownerName}))
	})
	It("should detect error if the list query returns error", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectQuery(expectedListStmt).WithArgs("pg_").WillReturnError(fmt.Errorf("boom"))
		tbs, err := List(ctx, db)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(tbs).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("should issue the expected command to create a tablespace", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		tbsName := "atablespace"
		ownerName := "postgres"
		stmt := fmt.Sprintf(expectedCreateStmt, tbsName, ownerName)
		mock.ExpectExec(stmt).
			WillReturnResult(sqlmock.NewResult(2, 1))
		err = Create(ctx, db, Tablespace{Name: tbsName, Owner: ownerName})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("should detect error if database errors on tablespace creation", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		tbsName := "atablespace"
		ownerName := "postgres"
		stmt := fmt.Sprintf(expectedCreateStmt, tbsName, ownerName)
		mock.ExpectExec(stmt).
			WillReturnError(fmt.Errorf("boom"))
		err = Create(ctx, db, Tablespace{Name: tbsName, Owner: ownerName})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("should issue the expected command to update a tablespace", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		tbsName := "atablespace"
		ownerName := "postgres"
		stmt := fmt.Sprintf(expectedUpdateStmt, tbsName, ownerName)
		mock.ExpectExec(stmt).
			WillReturnResult(sqlmock.NewResult(2, 1))
		err = Update(ctx, db, Tablespace{Name: tbsName, Owner: ownerName})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})
