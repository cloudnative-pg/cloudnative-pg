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
	"database/sql/driver"

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

		It("should bind every released SHOW POOLS column to its own gauge", func() {
			// Column list of PgBouncer 1.25.2, src/admin.c admin_show_pools(). The gauge names are
			// spelled out rather than derived from byColumn, so a renamed or re-pointed gauge fails
			// here instead of being confirmed by the code under test.
			gaugeNames := map[string]string{
				"cl_active":             "cnpg_pgbouncer_pools_cl_active",
				"cl_waiting":            "cnpg_pgbouncer_pools_cl_waiting",
				"cl_active_cancel_req":  "cnpg_pgbouncer_pools_cl_active_cancel_req",
				"cl_waiting_cancel_req": "cnpg_pgbouncer_pools_cl_waiting_cancel_req",
				"sv_active":             "cnpg_pgbouncer_pools_sv_active",
				"sv_active_cancel":      "cnpg_pgbouncer_pools_sv_active_cancel",
				"sv_being_canceled":     "cnpg_pgbouncer_pools_sv_wait_cancels",
				"sv_idle":               "cnpg_pgbouncer_pools_sv_idle",
				"sv_used":               "cnpg_pgbouncer_pools_sv_used",
				"sv_tested":             "cnpg_pgbouncer_pools_sv_tested",
				"sv_login":              "cnpg_pgbouncer_pools_sv_login",
				"maxwait":               "cnpg_pgbouncer_pools_maxwait",
				"maxwait_us":            "cnpg_pgbouncer_pools_maxwait_us",
			}
			columns := []string{
				"database", "user",
				"cl_active", "cl_waiting",
				"cl_active_cancel_req",
				"cl_waiting_cancel_req",
				"sv_active",
				"sv_active_cancel",
				"sv_being_canceled",
				"sv_idle",
				"sv_used", "sv_tested",
				"sv_login", "maxwait",
				"maxwait_us", "pool_mode",
				"load_balance_hosts",
			}

			// A distinct value per numeric column, so a gauge bound to the wrong column is caught.
			row := make([]driver.Value, len(columns))
			row[0], row[1] = "db1", "user1"
			for i := 2; i < len(columns)-2; i++ {
				row[i] = i
			}
			row[len(columns)-2] = "transaction"
			row[len(columns)-1] = "round-robin"
			mock.ExpectQuery("SHOW POOLS;").WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))

			// A pedantic registry also fails on a gauge that Collect emits without Describe.
			poolsRegistry := prometheus.NewPedanticRegistry()
			poolsRegistry.MustRegister(exp.Metrics.ShowPools)

			exp.collectShowPools(ch, db)

			metrics, err := poolsRegistry.Gather()
			Expect(err).ToNot(HaveOccurred())

			errorValue := getMetric(metrics, lastCollectionErrorKey)
			Expect(errorValue).To(BeNil(), "a scan error was recorded")

			for i, column := range columns {
				name, isNumeric := gaugeNames[column]
				if !isNumeric {
					continue
				}
				family := getMetric(metrics, name)
				Expect(family).ToNot(BeNil(), "no metric exported for column %s", column)
				Expect(family.GetMetric()).To(HaveLen(1))
				Expect(family.GetMetric()[0].GetGauge().GetValue()).
					To(BeEquivalentTo(i), "wrong gauge bound to column %s", column)
			}

			// The two text columns carry an encoded enum rather than the scanned number.
			poolMode := getMetric(metrics, "cnpg_pgbouncer_pools_pool_mode")
			Expect(poolMode).ToNot(BeNil())
			Expect(poolMode.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(2))

			loadBalanceHosts := getMetric(metrics, "cnpg_pgbouncer_pools_load_balance_hosts")
			Expect(loadBalanceHosts).ToNot(BeNil())
			Expect(loadBalanceHosts.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(2))
		})

		It("should keep binding by name when PgBouncer reports unknown columns", func() {
			mock.ExpectQuery("SHOW POOLS;").WillReturnRows(
				sqlmock.NewRows([]string{
					"database",
					"user",
					"cl_active",
					"sv_future_count",
					"sv_idle",
				}).AddRow("db1", "user1", 1, 99, 2))

			poolsRegistry := prometheus.NewPedanticRegistry()
			poolsRegistry.MustRegister(exp.Metrics.ShowPools)

			exp.collectShowPools(ch, db)

			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			errorValue := getMetric(metrics, lastCollectionErrorKey).GetMetric()[0].GetGauge().GetValue()
			Expect(errorValue).To(BeEquivalentTo(0))

			poolsMetrics, err := poolsRegistry.Gather()
			Expect(err).ToNot(HaveOccurred())

			clActive := getMetric(poolsMetrics, "cnpg_pgbouncer_pools_cl_active")
			Expect(clActive).ToNot(BeNil())
			Expect(clActive.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))

			svIdle := getMetric(poolsMetrics, "cnpg_pgbouncer_pools_sv_idle")
			Expect(svIdle).ToNot(BeNil())
			Expect(svIdle.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(2))
		})

		It("should return the correct integer value", func() {
			Expect(poolModeToInt("session")).To(Equal(1))
			Expect(poolModeToInt("transaction")).To(Equal(2))
			Expect(poolModeToInt("statement")).To(Equal(3))
			Expect(poolModeToInt("random")).To(Equal(-1))

			Expect(loadBalanceHostsToInt(sql.NullString{})).To(Equal(0))
			Expect(loadBalanceHostsToInt(sql.NullString{String: "disable", Valid: true})).To(Equal(1))
			Expect(loadBalanceHostsToInt(sql.NullString{String: "round-robin", Valid: true})).To(Equal(2))
			Expect(loadBalanceHostsToInt(sql.NullString{String: "random", Valid: true})).To(Equal(-1))
		})
	})
})
