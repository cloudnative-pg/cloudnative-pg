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

package infrastructure

import (
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Postgres tablespaces functions test", func() {
	expectedListStmt := `SELECT spcname, 
		CASE WHEN spcname=ANY(regexp_split_to_array(current_setting('TEMP_TABLESPACES'),E'\\s*,\\s*')) 
		THEN true ELSE false END AS temp 
	 FROM pg_tablespace  
	 WHERE spcname NOT IN ('pg_default','pg_global')`
	expectedCreateStmt := "CREATE TABLESPACE \"%s\" LOCATION '%s'"
	It("should send the expected query to list tablespaces and parse the return", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		tbsManager := newPostgresTablespaceManager(db)
		rows := sqlmock.NewRows(
			[]string{"spcname", "temp"}).
			AddRow("atablespace", false).
			AddRow("anothertablespace", true)
		mock.ExpectQuery(expectedListStmt).WillReturnRows(rows)
		tbs, err := tbsManager.List(ctx)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(tbs).To(HaveLen(2))
		Expect(tbs).To(ConsistOf(
			Tablespace{Name: "atablespace"},
			Tablespace{Name: "anothertablespace", Temporary: true}))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("should detect error if the list query returns error", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		tbsManager := newPostgresTablespaceManager(db)
		mock.ExpectQuery(expectedListStmt).WillReturnError(fmt.Errorf("boom"))
		tbs, err := tbsManager.List(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(tbs).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("should issue the expected command to create a tablespace", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		tbsName := "atablespace"
		stmt := fmt.Sprintf(expectedCreateStmt, tbsName, specs.LocationForTablespace(tbsName))
		tbsManager := newPostgresTablespaceManager(db)
		mock.ExpectExec(stmt).WillReturnResult(sqlmock.NewResult(2, 1))
		err = tbsManager.Create(ctx, Tablespace{Name: tbsName})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("should detect error if database errors on tablespace creation", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		tbsName := "atablespace"
		stmt := fmt.Sprintf(expectedCreateStmt, tbsName, specs.LocationForTablespace(tbsName))
		tbsManager := newPostgresTablespaceManager(db)
		mock.ExpectExec(stmt).WillReturnError(fmt.Errorf("boom"))
		err = tbsManager.Create(ctx, Tablespace{Name: tbsName})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})
