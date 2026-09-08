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

package webserver

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("stopping an online backup", func() {
	It("captures pg_control before calling pg_backup_stop", func(ctx SpecContext) {
		pgData := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(pgData, "global"), 0o700)).To(Succeed())

		pgControl := []byte("pg_control")
		Expect(os.WriteFile(
			filepath.Join(pgData, "global", constants.PgControlFile),
			pgControl,
			0o600,
		)).To(Succeed())

		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		conn, err := db.Conn(ctx)
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectQuery(
			"SELECT lsn, labelfile, spcmapfile FROM pg_catalog.pg_backup_stop(wait_for_archive => $1);",
		).
			WithArgs(true).
			WillReturnRows(sqlmock.NewRows([]string{"lsn", "labelfile", "spcmapfile"}).
				AddRow("0/12345678", []byte("backup label"), []byte("tablespace map")))
		mock.ExpectClose()

		backup := &backupConnection{
			waitForArchive:       true,
			conn:                 conn,
			postgresMajorVersion: 15,
			pgData:               pgData,
		}
		backup.stopBackup(ctx, &sync.Mutex{})

		Expect(backup.err).ToNot(HaveOccurred())
		Expect(backup.data.PgControlFile).To(Equal(pgControl))
		Expect(backup.data.Phase).To(Equal(Completed))
		Expect(db.Close()).To(Succeed())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("does not stop the backup when pg_control cannot be captured", func(ctx SpecContext) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		conn, err := db.Conn(ctx)
		Expect(err).ToNot(HaveOccurred())
		mock.ExpectClose()

		backup := &backupConnection{
			conn:                 conn,
			postgresMajorVersion: 15,
			pgData:               GinkgoT().TempDir(),
		}
		backup.stopBackup(ctx, &sync.Mutex{})

		Expect(backup.err).To(MatchError(ContainSubstring(
			"while reading pg_control before stopping the backup",
		)))
		Expect(db.Close()).To(Succeed())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})
