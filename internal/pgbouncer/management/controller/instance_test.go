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

package controller

import (
	"database/sql"
	"sync"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PgBouncerInstance", func() {
	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
		err  error
	)

	BeforeEach(func() {
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	Context("when the instance is paused", func() {
		It("should not return an error", func() {
			mock.ExpectExec("PAUSE").WillReturnResult(sqlmock.NewResult(1, 1))

			pgBouncerInstance := &pgBouncerInstance{
				mu:     &sync.RWMutex{},
				paused: false,
				pool:   &fakePooler{DB: db},
			}

			err := pgBouncerInstance.Pause()
			Expect(err).NotTo(HaveOccurred())
			Expect(pgBouncerInstance.Paused()).To(BeTrue())
		})
	})

	Context("when the instance is resumed", func() {
		It("should not return an error", func() {
			mock.ExpectExec("RESUME").WillReturnResult(sqlmock.NewResult(1, 1))

			pgBouncerInstance := &pgBouncerInstance{
				mu:     &sync.RWMutex{},
				paused: true,
				pool:   &fakePooler{DB: db},
			}

			err := pgBouncerInstance.Resume()
			Expect(err).NotTo(HaveOccurred())
			Expect(pgBouncerInstance.Paused()).To(BeFalse())
		})
	})

	Context("when the instance configuration is reloaded", func() {
		It("should not return an error", func() {
			mock.ExpectExec("RELOAD").WillReturnResult(sqlmock.NewResult(1, 1))

			pgBouncerInstance := &pgBouncerInstance{
				mu:     &sync.RWMutex{},
				paused: false,
				pool:   &fakePooler{DB: db},
			}

			err := pgBouncerInstance.Reload()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

type fakePooler struct {
	DB *sql.DB
}

func (f *fakePooler) Connection(_ string) (*sql.DB, error) {
	return f.DB, nil
}

func (f *fakePooler) GetDsn(_ string) string {
	return "postgres://user:password@localhost:5432/testdb"
}

func (f *fakePooler) ShutdownConnections() {
}
