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

package utils

import (
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Credentials management functions", func() {
	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("will not disable the password if the PostgreSQL user has no password", func() {
		rowsHasPassword := sqlmock.NewRows([]string{""}).
			AddRow(false)
		mock.ExpectQuery(`SELECT rolpassword IS NOT NULL
		FROM pg_catalog.pg_authid
		WHERE rolname='postgres'`).WillReturnRows(rowsHasPassword)

		Expect(DisableSuperuserPassword(db)).To(Succeed())
	})

	It("will not disable the password if the PostgreSQL user doesn't exist", func() {
		rowsHasPassword := sqlmock.NewRows([]string{""})
		mock.ExpectQuery(`SELECT rolpassword IS NOT NULL
		FROM pg_catalog.pg_authid
		WHERE rolname='postgres'`).WillReturnRows(rowsHasPassword)

		Expect(DisableSuperuserPassword(db)).To(Succeed())
	})

	It("can disable the password for the PostgreSQL user", func() {
		rowsHasPassword := sqlmock.NewRows([]string{""}).
			AddRow(true)
		mock.ExpectQuery(`SELECT rolpassword IS NOT NULL
		FROM pg_catalog.pg_authid
		WHERE rolname='postgres'`).WillReturnRows(rowsHasPassword)
		mock.ExpectBegin()
		mock.ExpectExec("ALTER ROLE postgres WITH PASSWORD NULL").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		Expect(DisableSuperuserPassword(db)).To(Succeed())
	})

	It("can set the password for a PostgreSQL role", func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL log_min_error_statement = 'PANIC'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ALTER ROLE \"testuser\" WITH PASSWORD 'testpassword'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		Expect(SetUserPassword("testuser", "testpassword", db)).To(Succeed())
	})

	It("will correctly escape the password if needed", func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL log_min_error_statement = 'PANIC'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ALTER ROLE \"testuser\" WITH PASSWORD 'this \"is\" weird but ''possible'''").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		Expect(SetUserPassword("testuser", "this \"is\" weird but 'possible'", db)).To(Succeed())
	})

	It("will rollback setting of the password if there is an error", func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL log_min_error_statement = 'PANIC'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		dbError := errors.New("kaboom")
		mock.ExpectExec("ALTER ROLE \"testuser\" WITH PASSWORD 'this \"is\" weird but ''possible'''").
			WillReturnError(dbError)
		mock.ExpectRollback()
		err := SetUserPassword("testuser", "this \"is\" weird but 'possible'", db)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, dbError)).To(BeTrue())
	})
})
