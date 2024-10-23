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

// nolint: dupl
package controller

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"

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
})
