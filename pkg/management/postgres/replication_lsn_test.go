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
	"context"
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("replication LSN helpers", func() {
	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	Describe("getCurrentWALInsertLSN", func() {
		It("returns the insert LSN", func(ctx context.Context) {
			mock.ExpectQuery("pg_current_wal_insert_lsn").
				WillReturnRows(sqlmock.NewRows([]string{"lsn"}).AddRow("0/5000060"))

			lsn, err := getCurrentWALInsertLSN(ctx, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(lsn).To(Equal("0/5000060"))
		})

		It("wraps the query errors", func(ctx context.Context) {
			mock.ExpectQuery("pg_current_wal_insert_lsn").WillReturnError(errors.New("boom"))

			_, err := getCurrentWALInsertLSN(ctx, db)
			Expect(err).To(MatchError(ContainSubstring("boom")))
		})
	})

	Describe("countStandbysFlushedAtLeast", func() {
		It("does not query the database when there are no standbys", func(ctx context.Context) {
			count, err := countStandbysFlushedAtLeast(ctx, db, nil, "0/5000060")
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("returns the number of aligned standbys", func(ctx context.Context) {
			mock.ExpectQuery("FROM pg_catalog.pg_stat_replication").
				WithArgs(sqlmock.AnyArg(), "streaming_replica", "0/5000060").
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

			count, err := countStandbysFlushedAtLeast(ctx, db, []string{"c-2", "c-3", "c-4"}, "0/5000060")
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(2))
		})

		It("wraps the query errors", func(ctx context.Context) {
			mock.ExpectQuery("FROM pg_catalog.pg_stat_replication").
				WillReturnError(errors.New("boom"))

			_, err := countStandbysFlushedAtLeast(ctx, db, []string{"c-2"}, "0/5000060")
			Expect(err).To(MatchError(ContainSubstring("boom")))
		})
	})

	Describe("requestCheckpoint", func() {
		It("executes a checkpoint", func(ctx context.Context) {
			mock.ExpectExec("CHECKPOINT").WillReturnResult(sqlmock.NewResult(0, 0))

			Expect(requestCheckpoint(ctx, db)).To(Succeed())
		})

		It("wraps the errors", func(ctx context.Context) {
			mock.ExpectExec("CHECKPOINT").WillReturnError(errors.New("boom"))

			Expect(requestCheckpoint(ctx, db)).To(MatchError(ContainSubstring("boom")))
		})
	})

	Describe("hasCheckpointStartedAtOrAfter", func() {
		It("returns the comparison made by the database", func(ctx context.Context) {
			mock.ExpectQuery("FROM pg_catalog.pg_control_checkpoint").
				WithArgs("0/5000060").
				WillReturnRows(sqlmock.NewRows([]string{"started"}).AddRow(true))

			started, err := hasCheckpointStartedAtOrAfter(ctx, db, "0/5000060")
			Expect(err).ToNot(HaveOccurred())
			Expect(started).To(BeTrue())
		})

		It("returns false when the last checkpoint is older", func(ctx context.Context) {
			mock.ExpectQuery("FROM pg_catalog.pg_control_checkpoint").
				WithArgs("0/5000060").
				WillReturnRows(sqlmock.NewRows([]string{"started"}).AddRow(false))

			started, err := hasCheckpointStartedAtOrAfter(ctx, db, "0/5000060")
			Expect(err).ToNot(HaveOccurred())
			Expect(started).To(BeFalse())
		})

		It("wraps the query errors", func(ctx context.Context) {
			mock.ExpectQuery("FROM pg_catalog.pg_control_checkpoint").WillReturnError(errors.New("boom"))

			_, err := hasCheckpointStartedAtOrAfter(ctx, db, "0/5000060")
			Expect(err).To(MatchError(ContainSubstring("boom")))
		})
	})
})
