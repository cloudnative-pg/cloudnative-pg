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

package postgres

import (
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensure isWalArchiveWorking works correctly", func() {
	const flexibleCoalescenceQuery = "SELECT COALESCE.*FROM pg_stat_archiver"
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
			dbProviderFunc: func() (*sql.DB, error) {
				return db, nil
			},
		}
	})

	It("returns nil if WAL archiving is working", func() {
		bootstrapper.firstWalArchiveTriggered = true
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).
			AddRow(true, false)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)

		err := bootstrapper.tryBootstrapWal()
		Expect(err).To(BeNil())
		Expect(mock.ExpectationsWereMet()).To(BeNil())
	})

	It("returns an error if WAL archiving is not working and last_failed_time is present", func() {
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).
			AddRow(false, true)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)

		err := bootstrapper.tryBootstrapWal()
		Expect(err).To(Equal(errors.New("wal-archive not working")))
		Expect(mock.ExpectationsWereMet()).To(BeNil())
	})

	It("triggers the first WAL archive if it has not been triggered", func() {
		// set up mock expectations
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).AddRow(false, false)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)
		mock.ExpectExec("CHECKPOINT").WillReturnResult(fakeResult)
		mock.ExpectExec("SELECT pg_switch_wal()").WillReturnResult(fakeResult)

		bootstrapper.isPrimary = true

		// Call the function
		err := bootstrapper.tryBootstrapWal()
		Expect(err).To(Equal(errors.New("first wal-archive triggered")))

		// Ensure the mock expectations are met
		Expect(mock.ExpectationsWereMet()).To(BeNil())
	})
})
