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

package utils

import (
	"database/sql"

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

	It("can disable the password for the PostgreSQL user", func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL synchronous_commit to LOCAL").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ALTER ROLE postgres WITH PASSWORD NULL").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		Expect(DisableSuperuserPassword(db)).To(Succeed())
	})

	It("can set the password for a PostgreSQL role", func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL synchronous_commit to LOCAL").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ALTER ROLE \"testuser\" WITH PASSWORD 'testpassword'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		Expect(SetUserPassword("testuser", "testpassword", db)).To(Succeed())
	})

	It("will correctly escape the password if needed", func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET LOCAL synchronous_commit to LOCAL").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ALTER ROLE \"testuser\" WITH PASSWORD 'this \"is\" weird but ''possible'''").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		Expect(SetUserPassword("testuser", "this \"is\" weird but 'possible'", db)).To(Succeed())
	})
})
