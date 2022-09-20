/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Set default queries", func() {
	q := NewQueriesCollector("test", nil, "db")

	It("does assign nothing with empty default queries", func() {
		Expect(q.userQueries).To(BeEmpty())
		Expect(q.mappings).To(BeEmpty())
		Expect(q.variableLabels).To(BeEmpty())
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
			Expect(ch).To(HaveLen(0))
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
	})
})
