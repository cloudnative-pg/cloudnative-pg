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
	"context"
	"database/sql"
	"errors"
	"fmt"

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

	It("forwards an already encrypted password unchanged", func(ctx context.Context) {
		const scramHash = "SCRAM-SHA-256$4096:Y2F2YWxjYW50aQ==$" +
			"eCIyo2QEZvwlcMThm1zwQDPnw0jOHlCapCE+QFpHsGs=:" +
			"YKhSEcd4QiX3SBzmtTOHHA/9yaTBGJWAMMw7+92OyHM="

		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL log_statement = 'none'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("SET LOCAL log_min_error_statement = 'PANIC'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(fmt.Sprintf("ALTER ROLE \"testuser\" WITH PASSWORD '%s'", scramHash)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		Expect(SetUserPassword(ctx, "testuser", scramHash, db)).To(Succeed())
	})

	It("will rollback setting of the password if there is an error", func(ctx context.Context) {
		const scramHash = "SCRAM-SHA-256$4096:Y2F2YWxjYW50aQ==$" +
			"eCIyo2QEZvwlcMThm1zwQDPnw0jOHlCapCE+QFpHsGs=:" +
			"YKhSEcd4QiX3SBzmtTOHHA/9yaTBGJWAMMw7+92OyHM="

		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL log_statement = 'none'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("SET LOCAL log_min_error_statement = 'PANIC'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		dbError := errors.New("kaboom")
		mock.ExpectExec(fmt.Sprintf("ALTER ROLE \"testuser\" WITH PASSWORD '%s'", scramHash)).
			WillReturnError(dbError)
		mock.ExpectRollback()
		err := SetUserPassword(ctx, "testuser", scramHash, db)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(dbError))
	})

	It("SCRAM-encodes a plaintext password before sending it", func(ctx context.Context) {
		regexDB, regexMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		Expect(err).ToNot(HaveOccurred())

		regexMock.ExpectBegin()
		regexMock.ExpectExec("SET LOCAL log_statement = 'none'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		regexMock.ExpectExec("SET LOCAL log_min_error_statement = 'PANIC'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		regexMock.ExpectExec(`ALTER ROLE "testuser" WITH PASSWORD 'SCRAM-SHA-256\$.+'`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		regexMock.ExpectCommit()
		regexMock.ExpectClose()

		Expect(SetUserPassword(ctx, "testuser", "hunter2", regexDB)).To(Succeed())
		Expect(regexDB.Close()).To(Succeed())
		Expect(regexMock.ExpectationsWereMet()).To(Succeed())
	})
})
