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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Exporter", func() {
	var (
		registry  *prometheus.Registry
		db        *sql.DB
		mock      sqlmock.Sqlmock
		exp       *Exporter
		ch        chan prometheus.Metric
		columns16 = []string{
			"database",
			"user",
			"cl_active",
			"cl_waiting",
			"cl_active_cancel_req",
			"cl_waiting_cancel_req",
			"sv_active",
			"sv_active_cancel",
			"sv_being_canceled",
			"sv_idle",
			"sv_used",
			"sv_tested",
			"sv_login",
			"maxwait",
			"maxwait_us",
			"pool_mode",
		}
	)

	BeforeEach(func(ctx SpecContext) {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).ShouldNot(HaveOccurred())

		exp = &Exporter{
			Metrics: newMetrics(),
			pool:    fakePooler{db: db},
			ctx:     ctx,
		}

		registry = prometheus.NewRegistry()
		registry.MustRegister(exp.Metrics.PgbouncerUp)
		registry.MustRegister(exp.Metrics.Error)

		ch = make(chan prometheus.Metric, 1000)
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	Context("collectShowPools", func() {
		It("should react properly if SQL shows no pools", func() {
			mock.ExpectQuery("SHOW POOLS;").WillReturnError(sql.ErrNoRows)
			exp.collectShowPools(ch, db)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			pgBouncerUpValue := getMetric(metrics, pgBouncerUpKey).GetMetric()[0].GetGauge().GetValue()
			Expect(pgBouncerUpValue).Should(BeEquivalentTo(0))

			errorValue := getMetric(metrics, lastCollectionErrorKey).GetMetric()[0].GetGauge().GetValue()
			Expect(errorValue).To(BeEquivalentTo(1))
		})

		It("should handle SQL rows scanning properly", func() {
			mock.ExpectQuery("SHOW POOLS;").
				WillReturnRows(sqlmock.NewRows(columns16).
					AddRow("db1", "user1", 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, "session"))

			exp.collectShowPools(ch, db)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			pgBouncerUpValue := getMetric(metrics, pgBouncerUpKey).GetMetric()[0].GetGauge().GetValue()
			Expect(pgBouncerUpValue).Should(BeEquivalentTo(1))

			errorValue := getMetric(metrics, lastCollectionErrorKey).GetMetric()[0].GetGauge().GetValue()
			Expect(errorValue).To(BeEquivalentTo(0))
		})

		It("should handle error during SQL rows scanning", func() {
			mock.ExpectQuery("SHOW POOLS;").
				WillReturnRows(sqlmock.NewRows(columns16).
					AddRow("db1", "user1", "error", 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, "session"))

			exp.collectShowPools(ch, db)

			registry.MustRegister(exp.Metrics.PgCollectionErrors)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			pgBouncerUpValue := getMetric(metrics, pgBouncerUpKey).GetMetric()[0].GetGauge().GetValue()
			Expect(pgBouncerUpValue).Should(BeEquivalentTo(1))

			errorsMetric := getMetric(metrics, collectionErrorsTotalKey).GetMetric()[0]
			label := errorsMetric.GetLabel()[0]
			Expect(*label.Name).To(BeEquivalentTo("collector"))
			Expect(*label.Value).To(BeEquivalentTo("sql: Scan error on column index 2, name \"cl_active\": " +
				"converting driver.Value type string (\"error\") to a int: invalid syntax"))
			Expect(errorsMetric.GetCounter().GetValue()).To(BeEquivalentTo(1))
		})

		It("should return the correct integer value", func() {
			Expect(poolModeToInt("session")).To(Equal(1))
			Expect(poolModeToInt("transaction")).To(Equal(2))
			Expect(poolModeToInt("statement")).To(Equal(3))
			Expect(poolModeToInt("random")).To(Equal(-1))
		})
	})
})
