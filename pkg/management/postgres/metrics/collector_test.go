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
	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"

	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"

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

var _ = Describe("QueryCollector tests", func() {
	Context("collect metric tests", func() {
		It("should ensure that a metric without conversion is discarded", func() {
			qc := QueryCollector{}
			ch := make(chan prometheus.Metric, 10)
			metricMap := MetricMap{
				Name:       "MALFORMED_COUNTER",
				Discard:    true,
				Conversion: nil,
				Label:      false,
			}
			qc.collectConstMetric(
				metricMap,
				"COUNTER_TEST",
				[]string{"COUNTER_TEST"},
				ch,
			)
			Expect(ch).To(BeEmpty())
		})

		It("should ensure that a metric with a bad conversion value is discarded", func() {
			qc := QueryCollector{}
			ch := make(chan prometheus.Metric, 10)
			cm := ColumnMapping{Usage: COUNTER}
			cs := cm.ToMetricMap("TEST_COLUMN", "TEST_NAMESPACE", []string{"TEST_VARIABLE"})
			for _, metricMap := range cs {
				// int are not converted
				qc.collectConstMetric(metricMap, 12, []string{"TEST"}, ch)
			}
			Expect(ch).To(BeEmpty())
		})

		It("should ensure that a correctly formed counter is sent", func() {
			qc := QueryCollector{}
			ch := make(chan prometheus.Metric, 10)
			cm := ColumnMapping{Usage: COUNTER}
			cs := cm.ToMetricMap("TEST_COLUMN", "TEST_NAMESPACE", []string{"TEST_VARIABLE"})
			for _, metricMap := range cs {
				qc.collectConstMetric(metricMap, int64(12), []string{"TEST"}, ch)
			}
			Expect(ch).To(HaveLen(1))
			res := <-ch
			desc := res.Desc()
			Expect(desc).To(ContainSubstring("TEST_NAMESPACE_TEST_COLUMN"))
			Expect(desc).To(ContainSubstring("TEST_VARIABLE"))
		})

		Context("fetch label testing", func() {
			It("should correctly fetch the mapped labels", func() {
				qc := QueryCollector{
					columnMapping: map[string]MetricMap{
						"LABEL_ENABLED": {
							Label: true,
						},
						"LABEL_NOT_ENABLED": {
							Label: false,
						},
					},
				}
				labels, success := qc.collectLabels(
					[]string{"LABEL_ENABLED", "LABEL_NOT_ENABLED"},
					[]interface{}{"SHOULD_FETCH", "SHOULD_NOT_FETCH"},
				)
				Expect(success).To(BeTrue())
				Expect(labels).To(HaveLen(1))
				Expect(labels).To(ContainElements("SHOULD_FETCH"))
			})

			It("should report success false when the fetched data conversion is not supported", func() {
				qc := QueryCollector{
					columnMapping: map[string]MetricMap{
						"LABEL_ENABLED": {
							Label: true,
						},
					},
				}
				labels, success := qc.collectLabels(
					[]string{"LABEL_ENABLED"},
					// int is not supported
					[]interface{}{234},
				)

				Expect(success).To(BeFalse())
				Expect(labels).To(BeZero())
			})
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
