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
		db         *sql.DB
		mock       sqlmock.Sqlmock
		fakeResult = sqlmock.NewResult(0, 1)
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns nil if WAL archiving is working", func() {
		detector := walArchiveBootstrapper{firstWalArchiveTriggered: true}
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).
			AddRow(true, false)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)

		err := detector.ensureWalArchiveIsBootstrapped(db)
		Expect(err).To(BeNil())
		Expect(mock.ExpectationsWereMet()).To(BeNil())
	})

	It("returns an error if WAL archiving is not working and last_failed_time is present", func() {
		detector := walArchiveBootstrapper{}
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).
			AddRow(false, true)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)

		err := detector.ensureWalArchiveIsBootstrapped(db)
		Expect(err).To(Equal(errors.New("wal-archive not working")))
		Expect(mock.ExpectationsWereMet()).To(BeNil())
	})

	It("triggers the first WAL archive if it has not been triggered", func() {
		detector := walArchiveBootstrapper{}
		// set up mock expectations
		rows := sqlmock.NewRows([]string{"is_archiving", "last_failed_time_present"}).AddRow(false, false)
		mock.ExpectQuery(flexibleCoalescenceQuery).WillReturnRows(rows)
		mock.ExpectExec("CHECKPOINT").WillReturnResult(fakeResult)
		mock.ExpectExec("SELECT pg_switch_wal()").WillReturnResult(fakeResult)

		// Call the function
		err := detector.ensureWalArchiveIsBootstrapped(db)
		Expect(err).To(Equal(errors.New("first wal-archive triggered")))

		// Ensure the mock expectations are met
		Expect(mock.ExpectationsWereMet()).To(BeNil())
	})
})
