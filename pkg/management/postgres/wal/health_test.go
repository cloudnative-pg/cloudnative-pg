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

package wal

import (
	"context"
	"fmt"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WAL Health Checker", func() {
	Describe("Check", func() {
		It("should report healthy archive when last archived is more recent than last failed", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			archivedTime := time.Now().Add(-1 * time.Minute)
			failedTime := time.Now().Add(-5 * time.Minute)

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(archivedTime, failedTime, int64(3)))

			mock.ExpectQuery("SELECT.*pg_replication_slots").
				WillReturnRows(sqlmock.NewRows([]string{
					"slot_name", "retention_bytes",
				}))

			checker := NewHealthChecker(func() (int, error) {
				return 5, nil
			})

			status, err := checker.Check(context.Background(), db, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.ArchiveHealthy).To(BeTrue())
			Expect(status.PendingWALFiles).To(Equal(5))
			Expect(status.ArchiverFailedCount).To(Equal(int64(3)))
			Expect(status.LastArchivedTime).NotTo(BeNil())
			Expect(status.LastFailedTime).NotTo(BeNil())
		})

		It("should report unhealthy archive when last failed is more recent than last archived", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			archivedTime := time.Now().Add(-10 * time.Minute)
			failedTime := time.Now().Add(-1 * time.Minute)

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(archivedTime, failedTime, int64(10)))

			mock.ExpectQuery("SELECT.*pg_replication_slots").
				WillReturnRows(sqlmock.NewRows([]string{
					"slot_name", "retention_bytes",
				}))

			checker := NewHealthChecker(func() (int, error) {
				return 150, nil
			})

			status, err := checker.Check(context.Background(), db, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.ArchiveHealthy).To(BeFalse())
			Expect(status.PendingWALFiles).To(Equal(150))
			Expect(status.ArchiverFailedCount).To(Equal(int64(10)))
		})

		It("should report unhealthy when never archived but has failures", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			failedTime := time.Now().Add(-1 * time.Minute)

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(nil, failedTime, int64(5)))

			mock.ExpectQuery("SELECT.*pg_replication_slots").
				WillReturnRows(sqlmock.NewRows([]string{
					"slot_name", "retention_bytes",
				}))

			checker := NewHealthChecker(func() (int, error) {
				return 0, nil
			})

			status, err := checker.Check(context.Background(), db, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.ArchiveHealthy).To(BeFalse())
		})

		It("should report healthy when no failures have occurred", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			archivedTime := time.Now().Add(-1 * time.Minute)

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(archivedTime, nil, int64(0)))

			mock.ExpectQuery("SELECT.*pg_replication_slots").
				WillReturnRows(sqlmock.NewRows([]string{
					"slot_name", "retention_bytes",
				}))

			checker := NewHealthChecker(func() (int, error) {
				return 0, nil
			})

			status, err := checker.Check(context.Background(), db, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.ArchiveHealthy).To(BeTrue())
			Expect(status.ArchiverFailedCount).To(Equal(int64(0)))
		})

		It("should detect inactive replication slots with WAL retention", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(time.Now(), nil, int64(0)))

			mock.ExpectQuery("SELECT.*pg_replication_slots").
				WillReturnRows(sqlmock.NewRows([]string{
					"slot_name", "retention_bytes",
				}).
					AddRow("stale_slot_1", int64(1073741824)). // 1GB
					AddRow("stale_slot_2", int64(5368709120))) // 5GB

			checker := NewHealthChecker(func() (int, error) {
				return 10, nil
			})

			status, err := checker.Check(context.Background(), db, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.InactiveSlots).To(HaveLen(2))
			Expect(status.InactiveSlots[0].SlotName).To(Equal("stale_slot_1"))
			Expect(status.InactiveSlots[0].RetentionBytes).To(Equal(int64(1073741824)))
			Expect(status.InactiveSlots[1].SlotName).To(Equal("stale_slot_2"))
			Expect(status.InactiveSlots[1].RetentionBytes).To(Equal(int64(5368709120)))
		})

		It("should not query slots on replicas", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(time.Now(), nil, int64(0)))

			// No slot query expected for replicas

			checker := NewHealthChecker(func() (int, error) {
				return 0, nil
			})

			status, err := checker.Check(context.Background(), db, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.InactiveSlots).To(BeNil())
		})

		It("should return error when ready WAL count fails", func() {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = db.Close() }()

			mock.ExpectQuery("SELECT.*pg_stat_archiver").
				WillReturnRows(sqlmock.NewRows([]string{
					"last_archived_time", "last_failed_time", "failed_count",
				}).AddRow(time.Now(), nil, int64(0)))

			checker := NewHealthChecker(func() (int, error) {
				return 0, fmt.Errorf("directory not found")
			})

			status, err := checker.Check(context.Background(), db, false)
			// When any check fails, the health check returns an incomplete error.
			// Callers (like autoresize) should fail-open when status is nil.
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("incomplete"))
			Expect(status).To(BeNil())
		})
	})
})
