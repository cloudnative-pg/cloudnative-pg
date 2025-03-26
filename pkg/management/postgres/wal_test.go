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

package postgres

import (
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensure isWalArchiveWorking works correctly", func() {
	const flexibleCoalescenceQuery = "SELECT COALESCE.*FROM pg_catalog.pg_stat_archiver"
	var (
		db           *sql.DB
		mock         sqlmock.Sqlmock
		fakeResult   = sqlmock.NewResult(0, 1)
		bootstrapper walArchiveBootstrapper
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).ToNot(HaveOccurred())

		bootstrapper = walArchiveBootstrapper{
			walArchiveAnalyzer: walArchiveAnalyzer{
				dbFactory: func() (*sql.DB, error) {
					return db, nil
				},
			},
		}
	})

	It("returns nil if WAL archiving is working", func() {
		bootstrapper.firstWalShipped = true
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).
			AddRow(true, false)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)

		err := bootstrapper.mustHaveFirstWalArchived(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("returns an error if WAL archiving is not working and last_failed_time is present", func() {
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).
			AddRow(false, true)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)

		err := bootstrapper.mustHaveFirstWalArchived(db)
		Expect(err).To(Equal(errors.New("wal-archive not working")))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("triggers the first WAL archive if it has not been triggered", func() {
		// set up mock expectations
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).AddRow(false, false)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)
		mock.ExpectExec("CHECKPOINT").WillReturnResult(fakeResult)
		mock.ExpectExec("SELECT pg_catalog.pg_switch_wal()").WillReturnResult(fakeResult)

		// Call the function
		err := bootstrapper.mustHaveFirstWalArchived(db)
		Expect(err).To(Equal(errors.New("no wal-archive present")))
		err = bootstrapper.shipWalFile(db)
		Expect(err).ToNot(HaveOccurred())

		// Ensure the mock expectations are met
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})
