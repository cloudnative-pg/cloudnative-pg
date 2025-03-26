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

package metricserver

import (
	"database/sql"
	"strconv"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensures walSettings works correctly", func() {
	const (
		sha256                     = "random-sha"
		walSegmentSize     float64 = 16777216
		walKeepSize        float64 = 512
		minWalSize         float64 = 80
		maxWalSize         float64 = 1024
		maxSlotWalKeepSize float64 = -1
		walKeepSegments    float64 = 25
		query                      = `
SELECT name, setting FROM pg_catalog.pg_settings
WHERE pg_settings.name
IN ('wal_segment_size', 'min_wal_size', 'max_wal_size', 'wal_keep_size', 'wal_keep_segments', 'max_slot_wal_keep_size')`
	)
	var (
		walSegmentSizeStr     = strconv.FormatFloat(walSegmentSize, 'f', -1, 64)
		walKeepSizeStr        = strconv.FormatFloat(walKeepSize, 'f', -1, 64)
		minWalSizeStr         = strconv.FormatFloat(minWalSize, 'f', -1, 64)
		maxWalSizeStr         = strconv.FormatFloat(maxWalSize, 'f', -1, 64)
		maxSlotWalKeepSizeStr = strconv.FormatFloat(maxSlotWalKeepSize, 'f', -1, 64)
		walKeepSegmentsStr    = strconv.FormatFloat(walKeepSegments, 'f', -1, 64)
	)

	var (
		db             *sql.DB
		mock           sqlmock.Sqlmock
		err            error
		pgSettingsRows *sqlmock.Rows
	)

	BeforeEach(func() {
		pgSettingsRows = sqlmock.NewRows([]string{"name", "setting"}).
			AddRow("wal_segment_size", walSegmentSizeStr).
			AddRow("min_wal_size", minWalSizeStr).
			AddRow("max_wal_size", maxWalSizeStr).
			AddRow("max_slot_wal_keep_size", maxSlotWalKeepSizeStr)

		db, mock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = db.Close()
		})
	})

	It("should not trigger a synchronize if the config sha256 is the same", func() {
		mock.ExpectQuery(query)
		settings := walSettings{configSha256: sha256}
		err := settings.synchronize(db, sha256)
		Expect(err).ToNot(HaveOccurred())

		expected := walSettings{configSha256: sha256}
		Expect(settings).To(Equal(expected))
	})

	It("it should execute the query and return the walSettings on a sha256 change. Postgres 13>=", func() {
		pgSettingsRows.AddRow("wal_keep_size", walKeepSizeStr)

		mock.ExpectQuery(query).
			WillReturnRows(pgSettingsRows)

		settings := walSettings{}
		err := settings.synchronize(db, sha256)
		Expect(err).ToNot(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).To(Succeed())

		Expect(settings.configSha256).To(Equal(sha256))
		Expect(settings.walSegmentSize).To(Equal(walSegmentSize))
		Expect(settings.walKeepSizeNormalized).To(Equal(utils.ToBytes(walKeepSize) / walSegmentSize))
		Expect(settings.minWalSize).To(Equal(minWalSize))
		Expect(settings.maxWalSize).To(Equal(maxWalSize))
		Expect(settings.maxSlotWalKeepSize).To(Equal(maxSlotWalKeepSize))
	})

	It("it should execute the query and return the walSettings on a sha256 change. Postgres 13<", func() {
		pgSettingsRows.AddRow("wal_keep_segments", walKeepSegmentsStr)
		mock.ExpectQuery(query).WillReturnRows(pgSettingsRows)

		settings := walSettings{}
		err := settings.synchronize(db, sha256)
		Expect(err).ToNot(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).To(Succeed())

		Expect(settings.configSha256).To(Equal(sha256))
		Expect(settings.walSegmentSize).To(Equal(walSegmentSize))
		Expect(settings.walKeepSizeNormalized).To(Equal(walKeepSegments))
		Expect(settings.minWalSize).To(Equal(minWalSize))
		Expect(settings.maxWalSize).To(Equal(maxWalSize))
		Expect(settings.maxSlotWalKeepSize).To(Equal(maxSlotWalKeepSize))
	})
})
