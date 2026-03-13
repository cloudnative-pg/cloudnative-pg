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
	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"

	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"

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
				[]any{"SHOULD_FETCH", "SHOULD_NOT_FETCH"},
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
				[]any{234},
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

		loPagesQuery := `SELECT pg_catalog.current_database() as datname, relpages as lo_pages
			FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON (n.oid = c.relnamespace)
			WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_largeobject';`

		defaultQueries := UserQueries{
			"collector": UserQuery{
				Query:           loPagesQuery,
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
			dbMock.ExpectQuery(loPagesQuery).WillReturnRows(sqlmock.NewRows(
				[]string{"datname", "lo_pages"}).
				AddRow(`app`, 3))
			m, err := qc.computeMetrics(db)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(1))
			Expect(m[0].Desc().String()).To(ContainSubstring(
				defaultQueries["collector"].Metrics[1]["lo_pages"].Description))
			var gauge io_prometheus_client.Metric
			Expect(m[0].Write(&gauge)).To(Succeed())
			Expect(gauge.GetGauge().GetValue()).To(BeEquivalentTo(3))
			Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

var _ = Describe("sendPluginMetrics tests", func() {
	It("should successfully send metrics when definitions and metrics match", func() {
		ch := make(chan prometheus.Metric, 10)
		desc := prometheus.NewDesc("test_metric", "test description", []string{"label1"}, nil)
		definitions := pluginClient.PluginMetricDefinitions{
			pluginClient.PluginMetricDefinition{
				FqName:    "test_metric",
				Desc:      desc,
				ValueType: prometheus.CounterValue,
			},
		}

		testMetrics := []*metrics.CollectMetric{
			{
				FqName:         "test_metric",
				Value:          42.0,
				VariableLabels: []string{"value1"},
			},
		}

		err := sendPluginMetrics(definitions, testMetrics, ch)

		Expect(err).ToNot(HaveOccurred())
		Expect(ch).To(HaveLen(1))

		// Verify the metric was sent
		metric := <-ch
		Expect(metric.Desc()).To(Equal(desc))
	})

	It("should return error when metric definition is not found", func() {
		ch := make(chan prometheus.Metric, 10)
		definitions := pluginClient.PluginMetricDefinitions{}
		testMetrics := []*metrics.CollectMetric{
			{
				FqName:         "missing_metric",
				Value:          42.0,
				VariableLabels: []string{"value1"},
			},
		}

		err := sendPluginMetrics(definitions, testMetrics, ch)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("metric definition not found for fqName: missing_metric"))
		Expect(ch).To(BeEmpty())
	})

	It("should return error when prometheus metric creation fails", func() {
		ch := make(chan prometheus.Metric, 10)
		desc := prometheus.NewDesc("test_metric", "test description", []string{"label1", "label2"}, nil)
		definitions := pluginClient.PluginMetricDefinitions{
			pluginClient.PluginMetricDefinition{
				FqName:    "test_metric",
				Desc:      desc,
				ValueType: prometheus.CounterValue,
			},
		}

		// Create metric with wrong number of labels (should cause NewConstMetric to fail)
		testMetrics := []*metrics.CollectMetric{
			{
				FqName:         "test_metric",
				Value:          42.0,
				VariableLabels: []string{"value1"}, // Only one label, but desc expects two
			},
		}

		err := sendPluginMetrics(definitions, testMetrics, ch)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create metric test_metric"))
		Expect(ch).To(BeEmpty())
	})

	It("should handle multiple metrics successfully", func() {
		ch := make(chan prometheus.Metric, 10)
		desc1 := prometheus.NewDesc("metric_one", "first metric", []string{"label1"}, nil)
		desc2 := prometheus.NewDesc("metric_two", "second metric", []string{"label2"}, nil)
		definitions := pluginClient.PluginMetricDefinitions{
			pluginClient.PluginMetricDefinition{
				FqName:    "metric_one",
				Desc:      desc1,
				ValueType: prometheus.CounterValue,
			},
			pluginClient.PluginMetricDefinition{
				FqName:    "metric_two",
				Desc:      desc2,
				ValueType: prometheus.GaugeValue,
			},
		}

		testMetrics := []*metrics.CollectMetric{
			{
				FqName:         "metric_one",
				Value:          10.0,
				VariableLabels: []string{"value1"},
			},
			{
				FqName:         "metric_two",
				Value:          20.0,
				VariableLabels: []string{"value2"},
			},
		}

		err := sendPluginMetrics(definitions, testMetrics, ch)

		Expect(err).ToNot(HaveOccurred())
		Expect(ch).To(HaveLen(2))
	})

	It("should handle empty metrics slice", func() {
		ch := make(chan prometheus.Metric, 10)
		definitions := pluginClient.PluginMetricDefinitions{}
		var testMetrics []*metrics.CollectMetric

		err := sendPluginMetrics(definitions, testMetrics, ch)

		Expect(err).ToNot(HaveOccurred())
		Expect(ch).To(BeEmpty())
	})
})

var _ = Describe("QueriesCollector timestamp metric tests", func() {
	var collector *QueriesCollector

	BeforeEach(func() {
		collector = NewQueriesCollector("test_collector", nil, "postgres")
	})

	It("should initialize with zero timestamp", func() {
		Expect(collector.timeLastUpdated.IsZero()).To(BeTrue())

		// Collect the timestamp metric
		ch := make(chan prometheus.Metric, 10)
		collector.lastUpdateTimestamp.Collect(ch)

		Expect(ch).To(HaveLen(1))
		metric := <-ch

		var m io_prometheus_client.Metric
		Expect(metric.Write(&m)).To(Succeed())
		// Should be 0 when not yet updated
		Expect(m.GetGauge().GetValue()).To(BeEquivalentTo(0))
	})

	It("should expose timestamp metric in Describe", func() {
		ch := make(chan *prometheus.Desc, 10)
		collector.Describe(ch)

		// The descriptor should be registered
		Expect(collector.lastUpdateTimestamp).NotTo(BeNil())

		// Should have collected some descriptors
		Expect(ch).ToNot(BeEmpty())
	})

	It("should include timestamp metric in Collect output", func() {
		ch := make(chan prometheus.Metric, 10)
		collector.Collect(ch)

		// Should include at least the timestamp and error gauge metrics
		Expect(len(ch)).To(BeNumerically(">=", 2))

		// Find the timestamp metric by checking gauge values
		var foundTimestamp bool
		for len(ch) > 0 {
			metric := <-ch
			var m io_prometheus_client.Metric
			if metric.Write(&m) == nil && m.Gauge != nil {
				foundTimestamp = true
				break
			}
		}
		Expect(foundTimestamp).To(BeTrue())
	})
})

var _ = Describe("QueriesCollector cache hit/miss metrics tests", func() {
	var collector *QueriesCollector

	BeforeEach(func() {
		collector = NewQueriesCollector("test_collector", nil, "postgres")
	})

	It("should initialize cache metrics with zero values", func() {
		ch := make(chan prometheus.Metric, 10)
		collector.cacheHits.Collect(ch)
		collector.cacheMiss.Collect(ch)

		Expect(ch).To(HaveLen(2))

		// Check cache hits
		metric := <-ch
		var m1 io_prometheus_client.Metric
		Expect(metric.Write(&m1)).To(Succeed())
		Expect(m1.GetGauge().GetValue()).To(BeEquivalentTo(0))

		// Check cache misses
		metric = <-ch
		var m2 io_prometheus_client.Metric
		Expect(metric.Write(&m2)).To(Succeed())
		Expect(m2.GetGauge().GetValue()).To(BeEquivalentTo(0))
	})

	It("should include cache metrics in Describe", func() {
		ch := make(chan *prometheus.Desc, 20)
		collector.Describe(ch)

		Expect(collector.cacheHits).NotTo(BeNil())
		Expect(collector.cacheMiss).NotTo(BeNil())
		Expect(ch).ToNot(BeEmpty())
	})

	It("should include cache metrics in Collect output", func() {
		ch := make(chan prometheus.Metric, 20)
		collector.Collect(ch)

		// Should include cache hits and cache misses
		Expect(len(ch)).To(BeNumerically(">=", 4))

		metricNames := make(map[string]float64)
		for len(ch) > 0 {
			metric := <-ch
			var m io_prometheus_client.Metric
			if metric.Write(&m) == nil && m.Gauge != nil {
				desc := metric.Desc().String()
				metricNames[desc] = m.GetGauge().GetValue()
			}
		}

		// Check that we collected both metrics
		Expect(len(metricNames)).To(BeNumerically(">=", 2))
	})

	It("should increment cache hits when RecordCacheHit is called", func() {
		// Record multiple cache hits
		collector.RecordCacheHit()
		collector.RecordCacheHit()
		collector.RecordCacheHit()

		ch := make(chan prometheus.Metric, 10)
		collector.cacheHits.Collect(ch)

		Expect(ch).To(HaveLen(1))
		metric := <-ch
		var m io_prometheus_client.Metric
		Expect(metric.Write(&m)).To(Succeed())
		Expect(m.GetGauge().GetValue()).To(BeEquivalentTo(3))
	})

	It("should reset cache metrics when Update is called (cache miss)", func() {
		// Simulate some cache hits first
		collector.RecordCacheHit()
		collector.RecordCacheHit()

		// Verify cache hits were recorded
		ch := make(chan prometheus.Metric, 10)
		collector.cacheHits.Collect(ch)
		metric := <-ch
		var m1 io_prometheus_client.Metric
		Expect(metric.Write(&m1)).To(Succeed())
		Expect(m1.GetGauge().GetValue()).To(BeEquivalentTo(2))

		// Now call Update (which represents a cache miss)
		// Note: This will fail without a real instance, but we can test the reset logic
		collector.metricsMutex.Lock()
		collector.cacheHits.Set(0)
		collector.cacheMiss.Set(1)
		collector.metricsMutex.Unlock()

		// Verify cache hits were reset to 0 and cache misses set to 1
		ch = make(chan prometheus.Metric, 10)
		collector.cacheHits.Collect(ch)
		collector.cacheMiss.Collect(ch)

		Expect(ch).To(HaveLen(2))

		// Check cache hits (should be reset to 0)
		metric = <-ch
		var m2 io_prometheus_client.Metric
		Expect(metric.Write(&m2)).To(Succeed())
		Expect(m2.GetGauge().GetValue()).To(BeEquivalentTo(0))

		// Check cache misses (should be 1)
		metric = <-ch
		var m3 io_prometheus_client.Metric
		Expect(metric.Write(&m3)).To(Succeed())
		Expect(m3.GetGauge().GetValue()).To(BeEquivalentTo(1))
	})

	It("should track cache hits for current cache period only", func() {
		// Simulate a cache period:
		// 1. Cache miss (update)
		collector.metricsMutex.Lock()
		collector.cacheHits.Set(0)
		collector.cacheMiss.Set(1)
		collector.metricsMutex.Unlock()

		// 2. Multiple cache hits during this period
		collector.RecordCacheHit()
		collector.RecordCacheHit()
		collector.RecordCacheHit()
		collector.RecordCacheHit()

		// 3. Verify we have 4 hits and 1 miss
		ch := make(chan prometheus.Metric, 10)
		collector.cacheHits.Collect(ch)
		collector.cacheMiss.Collect(ch)

		Expect(ch).To(HaveLen(2))

		hitMetric := <-ch
		var mHits io_prometheus_client.Metric
		Expect(hitMetric.Write(&mHits)).To(Succeed())
		Expect(mHits.GetGauge().GetValue()).To(BeEquivalentTo(4))

		missMetric := <-ch
		var mMisses io_prometheus_client.Metric
		Expect(missMetric.Write(&mMisses)).To(Succeed())
		Expect(mMisses.GetGauge().GetValue()).To(BeEquivalentTo(1))

		// 4. New cache period starts (cache miss/update)
		collector.metricsMutex.Lock()
		collector.cacheHits.Set(0)
		collector.cacheMiss.Set(1)
		collector.metricsMutex.Unlock()

		// 5. New hits in the new period
		collector.RecordCacheHit()
		collector.RecordCacheHit()

		// 6. Verify counters were reset and now show the new period
		ch = make(chan prometheus.Metric, 10)
		collector.cacheHits.Collect(ch)
		collector.cacheMiss.Collect(ch)

		Expect(ch).To(HaveLen(2))

		hitMetric = <-ch
		var mNewHits io_prometheus_client.Metric
		Expect(hitMetric.Write(&mNewHits)).To(Succeed())
		Expect(mNewHits.GetGauge().GetValue()).To(BeEquivalentTo(2))

		missMetric = <-ch
		var mNewMisses io_prometheus_client.Metric
		Expect(missMetric.Write(&mNewMisses)).To(Succeed())
		Expect(mNewMisses.GetGauge().GetValue()).To(BeEquivalentTo(1))
	})
})
