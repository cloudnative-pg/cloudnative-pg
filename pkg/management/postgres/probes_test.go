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
	"fmt"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blang/semver"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("probes", func() {
	It("fillWalStatus should properly handle errors", func() {
		instance := &Instance{}
		status := &postgres.PostgresqlStatus{
			IsPrimary: true,
		}

		db, mock, err := sqlmock.New()
		Expect(err).ToNot(HaveOccurred())

		errFailedQuery := fmt.Errorf("failed query")

		mock.ExpectQuery(
			regexp.QuoteMeta(`SELECT
				application_name,
				coalesce(state, ''),
				coalesce(sent_lsn::text, ''),
				coalesce(write_lsn::text, ''),
				coalesce(flush_lsn::text, ''),
				coalesce(replay_lsn::text, ''),
				coalesce(write_lag, '0'::interval),
				coalesce(flush_lag, '0'::interval),
				coalesce(replay_lag, '0'::interval),
				coalesce(sync_state, ''),
				coalesce(sync_priority, 0)
			FROM pg_catalog.pg_stat_replication
			WHERE application_name ~ $1 AND usename = $2`),
		).WithArgs("-[0-9]+$", "streaming_replica").WillReturnError(errFailedQuery)

		err = instance.fillWalStatusFromConnection(status, db)
		Expect(err).To(Equal(errFailedQuery))
	})

	It("fillArchiveStatus should properly handle errors", func() {
		db, mock, err := sqlmock.New()
		Expect(err).ToNot(HaveOccurred())

		mock.ExpectQuery(`.*`).
			WillReturnRows(sqlmock.NewRows([]string{
				"last_archived_wal",
				"last_archived_time",
				"last_failed_wal",
				"last_failed_time",
				"is_archiving",
			},
			).AddRow("000000010000000000000001", "2021-05-05 12:00:00", "", "2021-05-05 12:00:00", false))

		status := &postgres.PostgresqlStatus{}
		err = fillArchiverStatus(db, status)
		Expect(err).ToNot(HaveOccurred())

		Expect(mock.ExpectationsWereMet()).To(Succeed())

		Expect(status.LastArchivedWAL).To(Equal("000000010000000000000001"))
		Expect(status.LastArchivedWALTime).To(Equal("2021-05-05 12:00:00"))
		Expect(status.LastFailedWAL).To(Equal(""))
		Expect(status.LastFailedWALTime).To(Equal("2021-05-05 12:00:00"))
		Expect(status.IsArchivingWAL).To(BeFalse())
	})

	Context("Fill basebackup stats", func() {
		It("does nothing in case of that major version is less than 13 ", func() {
			instance := &Instance{
				pgVersion: &semver.Version{Major: 12},
			}
			Expect(instance.fillBasebackupStats(nil, nil)).To(Succeed())
		})

		It("set the information", func() {
			instance := (&Instance{
				pgVersion: &semver.Version{Major: 13},
			}).WithPodName("test-1")
			status := &postgres.PostgresqlStatus{
				IsPrimary: false,
			}

			db, mock, err := sqlmock.New()
			Expect(err).ToNot(HaveOccurred())

			mock.ExpectQuery(`.*`).
				WillReturnRows(sqlmock.NewRows([]string{
					"usename",
					"application_name",
					"backend_start",
					"phase",
					"backup_total",
					"backup_streamed",
					"backup_total_pretty",
					"backup_streamed_pretty",
					"tablespaces_total",
					"tablespaces_streamed",
				},
				).AddRow(
					"postgres",
					"pg_basebackup",
					"2021-05-05 12:00:00",
					"streaming database files",
					int64(1000),
					int64(200),
					"1000",
					"200",
					int64(2),
					int64(1),
				))

			Expect(instance.fillBasebackupStats(db, status)).To(Succeed())
			Expect(status.PgStatBasebackupsInfo).To(HaveLen(1))

			Expect(status.PgStatBasebackupsInfo[0].Usename).To(Equal("postgres"))
			Expect(status.PgStatBasebackupsInfo[0].ApplicationName).To(Equal("pg_basebackup"))
			Expect(status.PgStatBasebackupsInfo[0].BackendStart).To(Equal("2021-05-05 12:00:00"))
			Expect(status.PgStatBasebackupsInfo[0].Phase).To(Equal("streaming database files"))
			Expect(status.PgStatBasebackupsInfo[0].BackupTotal).To(Equal(int64(1000)))
			Expect(status.PgStatBasebackupsInfo[0].BackupStreamed).To(Equal(int64(200)))
			Expect(status.PgStatBasebackupsInfo[0].TablespacesTotal).To(Equal(int64(2)))
			Expect(status.PgStatBasebackupsInfo[0].TablespacesStreamed).To(Equal(int64(1)))
		})
	})
})
