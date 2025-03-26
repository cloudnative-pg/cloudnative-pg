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

package metricsserver

import (
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MetricsServer", func() {
	const (
		showStatsQuery = "SHOW STATS;"
	)

	var (
		db       *sql.DB
		mock     sqlmock.Sqlmock
		exp      *Exporter
		registry *prometheus.Registry
		ch       chan prometheus.Metric
	)

	BeforeEach(func(ctx SpecContext) {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())

		exp = &Exporter{
			Metrics: newMetrics(),
			pool:    fakePooler{db: db},
			ctx:     ctx,
		}
		registry = prometheus.NewRegistry()
		registry.MustRegister(exp.Metrics.Error)
		registry.MustRegister(exp.Metrics.ShowStats.TotalXactCount)
		registry.MustRegister(exp.Metrics.ShowStats.TotalQueryCount)
		registry.MustRegister(exp.Metrics.PgbouncerUp)

		ch = make(chan prometheus.Metric, 1000)
	})

	Context("collectShowStats", func() {
		It("should handle successful SQL query execution", func() {
			mock.ExpectQuery(showStatsQuery).WillReturnRows(getShowStatsRows())

			exp.collectShowStats(ch, db)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			pgbouncerUpMetric := getMetric(metrics, "cnpg_pgbouncer_up")
			Expect(pgbouncerUpMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))

			lastCollectionErrorMetric := getMetric(metrics, "cnpg_pgbouncer_last_collection_error")
			Expect(lastCollectionErrorMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(0))

			totalXactCountMetric := getMetric(metrics, "cnpg_pgbouncer_stats_total_xact_count")
			Expect(totalXactCountMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))

			totalQueryCountMetric := getMetric(metrics, "cnpg_pgbouncer_stats_total_query_count")
			Expect(totalQueryCountMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(2))
		})

		It("should handle error during SQL query execution", func() {
			mock.ExpectQuery(showStatsQuery).WillReturnError(errors.New("database error"))

			exp.collectShowStats(ch, db)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			pgbouncerUpMetric := getMetric(metrics, "cnpg_pgbouncer_up")
			Expect(pgbouncerUpMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(0))

			lastCollectionErrorMetric := getMetric(metrics, "cnpg_pgbouncer_last_collection_error")
			Expect(lastCollectionErrorMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))
		})

		It("should handle error during rows scanning", func() {
			mock.ExpectQuery(showStatsQuery).
				WillReturnRows(sqlmock.NewRows([]string{"total_xact_count"}).
					AddRow("invalid"))

			exp.collectShowStats(ch, db)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			pgbouncerUpMetric := getMetric(metrics, "cnpg_pgbouncer_up")
			Expect(pgbouncerUpMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))

			lastCollectionErrorMetric := getMetric(metrics, "cnpg_pgbouncer_last_collection_error")
			Expect(lastCollectionErrorMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))
		})
	})
})

func getShowStatsRows() *sqlmock.Rows {
	columns := []string{
		"database",
		"total_xact_count",
		"total_query_count",
		"total_received",
		"total_sent",
		"total_xact_time",
		"total_query_time",
		"total_wait_time",
		"avg_xact_count",
		"avg_query_count",
		"avg_recv",
		"avg_sent",
		"avg_xact_time",
		"avg_query_time",
		"avg_wait_time",
	}

	return sqlmock.NewRows(columns).
		AddRow("db1", 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14)
}
