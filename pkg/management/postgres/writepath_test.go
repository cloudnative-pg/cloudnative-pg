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
	"syscall"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("terminateWalReceiver", func() {
	const query = "SELECT pid FROM pg_catalog.pg_stat_wal_receiver LIMIT 1"

	var (
		db     *sql.DB
		mock   sqlmock.Sqlmock
		signal syscall.Signal
		killed []int
		killFn func(pid int, sig syscall.Signal) error
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			Expect(mock.ExpectationsWereMet()).To(Succeed())
			_ = db.Close()
		})

		killed = nil
		signal = 0
		killFn = func(pid int, sig syscall.Signal) error {
			killed = append(killed, pid)
			signal = sig
			return nil
		}
	})

	It("sends SIGTERM to the WAL receiver of the instance", func(ctx SpecContext) {
		mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(4242))

		terminated, err := terminateWalReceiver(ctx, db, killFn)
		Expect(err).ToNot(HaveOccurred())
		Expect(terminated).To(BeTrue())
		Expect(killed).To(Equal([]int{4242}))
		Expect(signal).To(Equal(syscall.SIGTERM))
	})

	It("does nothing when there is no WAL receiver", func(ctx SpecContext) {
		mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"pid"}))

		terminated, err := terminateWalReceiver(ctx, db, killFn)
		Expect(err).ToNot(HaveOccurred())
		Expect(terminated).To(BeFalse())
		Expect(killed).To(BeEmpty())
	})

	It("does nothing when the WAL receiver has no process", func(ctx SpecContext) {
		mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(nil))

		terminated, err := terminateWalReceiver(ctx, db, killFn)
		Expect(err).ToNot(HaveOccurred())
		Expect(terminated).To(BeFalse())
		Expect(killed).To(BeEmpty())
	})

	It("reports the failure of the query", func(ctx SpecContext) {
		mock.ExpectQuery(query).WillReturnError(errors.New("connection refused"))

		terminated, err := terminateWalReceiver(ctx, db, killFn)
		Expect(err).To(HaveOccurred())
		Expect(terminated).To(BeFalse())
		Expect(killed).To(BeEmpty())
	})

	It("is not an error when the process is already gone", func(ctx SpecContext) {
		mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(4242))

		terminated, err := terminateWalReceiver(ctx, db, func(int, syscall.Signal) error { return syscall.ESRCH })
		Expect(err).ToNot(HaveOccurred())
		Expect(terminated).To(BeTrue())
	})

	It("reports a failure to signal the process", func(ctx SpecContext) {
		mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(4242))

		terminated, err := terminateWalReceiver(ctx, db, func(int, syscall.Signal) error { return syscall.EPERM })
		Expect(err).To(MatchError(syscall.EPERM))
		Expect(terminated).To(BeFalse())
	})
})
