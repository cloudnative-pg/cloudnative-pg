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

package metrics

import (
	"database/sql"

	"github.com/DATA-DOG/go-sqlmock"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Set default queries", Ordered, func() {
	q := NewQueriesCollector("test", nil, "db")

	It("does assign nothing with empty default queries", func() {
		Expect(q.userQueries).To(BeEmpty())
		Expect(q.mappings).To(BeEmpty())
		Expect(q.variableLabels).To(BeEmpty())
	})

	It("properly works", func() {
		Expect(q.userQueries).To(BeEmpty())
		Expect(q.mappings).To(BeEmpty())
		Expect(q.variableLabels).To(BeEmpty())

		defaultQueries := UserQueries{
			"collector": UserQuery{
				Query: "SELECT FROM unit_tests",
				Metrics: []Mapping{
					{
						"test": ColumnMapping{
							Usage:       LABEL,
							Description: "test query",
						},
					},
				},
			},
		}

		q.InjectUserQueries(defaultQueries)
		Expect(len(q.userQueries)).To(BeEquivalentTo(1))
		Expect(len(q.mappings)).To(BeEquivalentTo(1))
		Expect(q.mappings["collector"]["test"].Name).To(BeEquivalentTo("test"))
		Expect(q.variableLabels["collector"]).To(BeEquivalentTo(VariableSet{"test"}))
	})
})

var _ = Describe("QueryRunner tests", func() {
	Context("collect metric tests", func() {
		It("should ensure that a metric without conversion is discarded", func() {
			qc := QueryRunner{}
			metricMap := MetricMap{
				Name:       "MALFORMED_COUNTER",
				Discard:    true,
				Conversion: nil,
				Label:      false,
			}
			m := qc.createConstMetric(
				metricMap,
				"COUNTER_TEST",
				[]string{"COUNTER_TEST"},
			)
			Expect(m).To(BeNil())
		})

		It("should ensure that a metric with a bad conversion value is discarded", func() {
			qc := QueryRunner{}
			cm := ColumnMapping{Usage: COUNTER}
			cs := cm.ToMetricMap("TEST_COLUMN", "TEST_NAMESPACE", []string{"TEST_VARIABLE"})
			for _, metricMap := range cs {
				// int are not converted
				m := qc.createConstMetric(metricMap, 12, []string{"TEST"})
				Expect(m).To(BeNil())
			}
		})

		It("should ensure that a correctly formed counter is sent", func() {
			qc := QueryRunner{}
			ch := make(chan prometheus.Metric, 10)
			cm := ColumnMapping{Usage: COUNTER}
			cs := cm.ToMetricMap("TEST_COLUMN", "TEST_NAMESPACE", []string{"TEST_VARIABLE"})
			for _, metricMap := range cs {
				m := qc.createConstMetric(metricMap, int64(12), []string{"TEST"})
				ch <- m
			}
			Expect(ch).To(HaveLen(1))
			res := <-ch
			desc := res.Desc()
			Expect(desc).To(ContainSubstring("TEST_NAMESPACE_TEST_COLUMN"))
			Expect(desc).To(ContainSubstring("TEST_VARIABLE"))
		})
	})

	Context("fetch label testing", func() {
		It("should correctly fetch the mapped labels", func() {
			qc := QueryRunner{
				columnMapping: map[string]MetricMap{
					"LABEL_ENABLED": {
						Label: true,
					},
					"LABEL_NOT_ENABLED": {
						Label: false,
					},
				},
			}
			labels, success := qc.listLabels(
				[]string{"LABEL_ENABLED", "LABEL_NOT_ENABLED"},
				[]interface{}{"SHOULD_FETCH", "SHOULD_NOT_FETCH"},
			)
			Expect(success).To(BeTrue())
			Expect(labels).To(HaveLen(1))
			Expect(labels).To(ContainElements("SHOULD_FETCH"))
		})

		It("should report success false when the fetched data conversion is not supported", func() {
			qc := QueryRunner{
				columnMapping: map[string]MetricMap{
					"LABEL_ENABLED": {
						Label: true,
					},
				},
			}
			labels, success := qc.listLabels(
				[]string{"LABEL_ENABLED"},
				// int is not supported
				[]interface{}{234},
			)

			Expect(success).To(BeFalse())
			Expect(labels).To(BeZero())
		})
	})

	Context("receiving query data from the database", func() {
		var (
			dbMock sqlmock.Sqlmock
			db     *sql.DB
			err    error
		)

		qry := `SELECT pg_catalog.current_database() as datname, relpages as lo_pages
			FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON (n.oid = c.relnamespace)
			WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_largeobject';`

		defaultQueries := UserQueries{
			"collector": UserQuery{
				Query:           qry,
				TargetDatabases: []string{"*"},
				Metrics: []Mapping{
					{
						"datname": ColumnMapping{
							Usage:       LABEL,
							Description: "Name of the database",
						},
					},
					{
						"lo_pages": ColumnMapping{
							Usage:       GAUGE,
							Description: "Estimated number of pages in the pg_largeobject table",
						},
					},
				},
			},
		}

		BeforeEach(func() {
			db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())
		})

		It("should compute the default metrics successfully", func() {
			metricMap := MetricMapSet{
				"datname": MetricMap{
					Name:       "datname",
					Discard:    true,
					Conversion: nil,
					Label:      true,
				},
				"lo_pages": MetricMap{
					Name:  "lo_pages",
					Vtype: prometheus.GaugeValue,
					Desc: prometheus.NewDesc(
						"collector_lo_pages",
						defaultQueries["collector"].Metrics[1]["lo_pages"].Description, []string{"lo_pages"}, nil),
					Conversion: postgresutils.DBToFloat64,
					Label:      false,
				},
			}

			qc := QueryRunner{
				namespace:      "foo",
				userQuery:      defaultQueries["collector"],
				columnMapping:  metricMap,
				variableLabels: []string{"foo"},
			}
			_ = metricMap
			dbMock.ExpectBegin()
			dbMock.ExpectExec("SET application_name TO cnpg_metrics_exporter").WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.ExpectExec("SET standard_conforming_strings TO on").WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.ExpectExec("SET ROLE TO pg_monitor").WillReturnResult(sqlmock.NewResult(0, 1))
			dbMock.ExpectQuery(qry).WillReturnRows(sqlmock.NewRows(
				[]string{"datname", "lo_pages"}).
				AddRow(`app`, 0))
			m, err := qc.computeMetrics(db)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(1))
			Expect(m[0].Desc().String()).To(ContainSubstring(
				defaultQueries["collector"].Metrics[1]["lo_pages"].Description))
			var foo io_prometheus_client.Metric
			Expect(m[0].Write(&foo)).To(Succeed())
			Expect(foo.GetGauge().GetValue()).To(BeEquivalentTo(0))
			Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		})
	})
})
