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
	"fmt"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blang/semver"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("areAllParamsUpdated", func() {
	It("should return true when all params match", func() {
		decreased := map[string]int{"max_connections": 90, "max_worker_processes": 4}
		controldata := map[string]int{"max_connections": 90, "max_worker_processes": 4}
		Expect(areAllParamsUpdated(decreased, controldata)).To(BeTrue())
	})

	It("should return false when a param doesn't match", func() {
		decreased := map[string]int{"max_connections": 90}
		controldata := map[string]int{"max_connections": 100}
		Expect(areAllParamsUpdated(decreased, controldata)).To(BeFalse())
	})

	It("should return false when a param is missing from controldata", func() {
		decreased := map[string]int{"max_connections": 90}
		controldata := map[string]int{}
		Expect(areAllParamsUpdated(decreased, controldata)).To(BeFalse())
	})

	It("should return true for empty decreased values", func() {
		decreased := map[string]int{}
		controldata := map[string]int{"max_connections": 100}
		Expect(areAllParamsUpdated(decreased, controldata)).To(BeTrue())
	})
})

var _ = Describe("updateResultForDecrease", func() {
	// decreasedSettingsQuery matches the SQL used by GetDecreasedSensibleSettings
	decreasedSettingsQuery := regexp.QuoteMeta(
		`SELECT pending_settings.name, CAST(coalesce(new_setting,default_setting) AS INTEGER) as new_setting`)

	Context("when there are no decreased standby-sensitive settings", func() {
		It("should not modify the result", func() {
			db, mock, err := sqlmock.New()
			Expect(err).ToNot(HaveOccurred())

			mock.ExpectQuery(decreasedSettingsQuery).
				WillReturnRows(sqlmock.NewRows([]string{"name", "new_setting"}))

			instance := &Instance{}
			result := &postgres.PostgresqlStatus{
				IsPrimary:      false,
				PendingRestart: true,
			}

			err = updateResultForDecrease(instance, db, result)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.PendingRestart).To(BeTrue())
			Expect(result.PendingRestartForDecrease).To(BeFalse())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Context("when this is a primary instance", func() {
		It("should keep PendingRestart true and set PendingRestartForDecrease", func() {
			db, mock, err := sqlmock.New()
			Expect(err).ToNot(HaveOccurred())

			mock.ExpectQuery(decreasedSettingsQuery).
				WillReturnRows(sqlmock.NewRows([]string{"name", "new_setting"}).
					AddRow("max_connections", 90))

			instance := &Instance{}
			result := &postgres.PostgresqlStatus{
				IsPrimary:      true,
				PendingRestart: true,
			}

			err = updateResultForDecrease(instance, db, result)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.PendingRestart).To(BeTrue())
			Expect(result.PendingRestartForDecrease).To(BeTrue())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Context("when this is a replica cluster instance", func() {
		DescribeTable("should keep PendingRestart true regardless of pod role",
			func(podName string, currentPrimary string) {
				db, mock, err := sqlmock.New()
				Expect(err).ToNot(HaveOccurred())

				mock.ExpectQuery(decreasedSettingsQuery).
					WillReturnRows(sqlmock.NewRows([]string{"name", "new_setting"}).
						AddRow("max_connections", 95))

				instance := (&Instance{}).WithPodName(podName)
				instance.SetCluster(&apiv1.Cluster{
					Spec: apiv1.ClusterSpec{
						ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
							Enabled: ptr.To(true),
							Source:  "cluster-example",
						},
					},
					Status: apiv1.ClusterStatus{
						CurrentPrimary: currentPrimary,
					},
				})

				result := &postgres.PostgresqlStatus{
					IsPrimary:      false,
					PendingRestart: true,
				}

				err = updateResultForDecrease(instance, db, result)
				Expect(err).ToNot(HaveOccurred())
				// In a replica cluster, PendingRestart should remain true
				// because pg_controldata values come from the external source
				// primary, not from this cluster
				Expect(result.PendingRestart).To(BeTrue())
				Expect(result.PendingRestartForDecrease).To(BeTrue())
				Expect(mock.ExpectationsWereMet()).To(Succeed())
			},
			Entry("designated primary",
				"cluster-replica-tls-1", "cluster-replica-tls-1"),
			Entry("non-designated-primary standby",
				"cluster-replica-tls-2", "cluster-replica-tls-1"),
		)
	})
})

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
