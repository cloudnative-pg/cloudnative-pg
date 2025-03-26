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
	"fmt"
	"math"

	"github.com/prometheus/client_golang/prometheus"

	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*
These unit tests cover the 'ToMetricMap' method of the 'ColumnMapping' structure.
'ToMetricMap' is responsible for converting a column mapping into a set of metrics
that can be collected and exported by Prometheus.

The tests are divided into several contexts, each corresponding to a possible usage
of the column (DISCARD, LABEL, COUNTER, GAUGE, HISTOGRAM, MAPPEDMETRIC, DURATION).
In each context, we simulate calling 'ToMetricMap' with the given usage and validate
the properties of the returned 'MetricMap'.

For some usage types (COUNTER, GAUGE, DURATION, MAPPEDMETRIC), additional tests are
performed to check the behavior of the conversion function returned within the 'MetricMap'.
These functions are responsible for converting the column's value from the database into a float64
value that can be exported by Prometheus. For each of these, we simulate calling the conversion
function with a value that should be able to be converted, and in some cases also with a value
that should not be able to be converted.

These tests ensure that the 'ToMetricMap' method handles each usage type correctly, and
that the conversion functions it returns also behave as expected.
*/
var _ = Describe("ColumnMapping ToMetricMap", func() {
	var (
		namespace      string
		variableLabels []string
	)

	BeforeEach(func() {
		namespace = "test_namespace"
		variableLabels = []string{"label1", "label2"}
	})

	Context("when usage is DISCARD", func() {
		It("should return expected MetricMapSet", func() {
			columnMapping := ColumnMapping{
				Usage: "DISCARD",
			}
			columnName := "discard_column"

			expected := MetricMapSet{
				columnName: {
					Name:       columnName,
					Discard:    true,
					Conversion: nil,
					Label:      false,
				},
			}

			Expect(columnMapping.ToMetricMap(columnName, namespace, variableLabels)).To(Equal(expected))
		})
	})

	Context("when usage is DURATION", func() {
		It("should return expected MetricMap for valid duration", func() {
			columnMapping := ColumnMapping{
				Usage:       "DURATION",
				Description: "Test duration",
			}
			columnName := "duration_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			val, ok := result[columnName].Conversion("2s")
			Expect(ok).To(BeTrue())
			Expect(val).To(BeEquivalentTo(2000.0))
		})

		It("should return NaN for invalid duration", func() {
			columnMapping := ColumnMapping{
				Usage:       "DURATION",
				Description: "Test duration",
			}
			columnName := "duration_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			val, ok := result[columnName].Conversion("invalid")
			Expect(ok).To(BeFalse())
			Expect(math.IsNaN(val)).To(BeTrue())
		})
	})

	Context("when usage is LABEL", func() {
		It("should return expected MetricMapSet", func() {
			columnMapping := ColumnMapping{
				Usage: "LABEL",
			}
			columnName := "label_column"

			expected := MetricMapSet{
				columnName: {
					Name:       columnName,
					Discard:    true,
					Conversion: nil,
					Label:      true,
				},
			}

			Expect(columnMapping.ToMetricMap(columnName, namespace, variableLabels)).To(Equal(expected))
		})
	})

	Context("when usage is COUNTER", func() {
		It("should return expected MetricMapSet", func() {
			columnMapping := ColumnMapping{
				Usage: "COUNTER",
			}
			columnName := "counter_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			// Validate the properties of the MetricMap
			Expect(result[columnName].Name).To(Equal(columnName))
			Expect(result[columnName].Vtype).To(Equal(prometheus.CounterValue))
			Expect(result[columnName].Desc.String()).To(Equal(prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil).String()))
			Expect(result[columnName].Label).To(BeFalse())

			// Test the behavior of the conversion function
			dbValue := "12345"
			expectedValue, _ := postgresutils.DBToFloat64(dbValue)
			output, ok := result[columnName].Conversion(dbValue)
			Expect(ok).To(BeTrue())
			Expect(output).To(Equal(expectedValue))
		})
	})

	Context("when usage is GAUGE", func() {
		It("should return expected MetricMap", func() {
			columnMapping := ColumnMapping{
				Usage:       "GAUGE",
				Description: "Test gauge",
			}
			columnName := "gauge_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			val, ok := result[columnName].Conversion("3.14")
			Expect(ok).To(BeTrue())
			Expect(val).To(BeEquivalentTo(3.14))
		})
	})

	Context("when usage is HISTOGRAM", func() {
		It("should return expected MetricMapSet", func() {
			columnMapping := ColumnMapping{
				Usage: "HISTOGRAM",
			}
			columnName := "histogram_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			Expect(result[columnName].Histogram).To(BeTrue())
			Expect(result[columnName+"_bucket"].Discard).To(BeTrue())
			Expect(result[columnName+"_sum"].Discard).To(BeTrue())
			Expect(result[columnName+"_count"].Discard).To(BeTrue())
		})
	})

	Context("when usage is MAPPEDMETRIC", func() {
		It("should return expected MetricMapSet", func() {
			columnMapping := ColumnMapping{
				Usage:   "MAPPEDMETRIC",
				Mapping: map[string]float64{"test": 1.0},
			}
			columnName := "mappedmetric_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			val, ok := result[columnName].Conversion("test")
			Expect(ok).To(BeTrue())
			Expect(val).To(BeEquivalentTo(1.0))
		})

		It("should return NaN for unknown mapping", func() {
			columnMapping := ColumnMapping{
				Usage:   "MAPPEDMETRIC",
				Mapping: map[string]float64{"test": 1.0},
			}
			columnName := "mappedmetric_column"

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)

			_, ok := result[columnName].Conversion("unknown")
			Expect(ok).To(BeFalse())
		})
	})

	Context("when overriding the column name", func() {
		It("should set the correct description", func() {
			customColumnName := "custom_column"
			columnName := "gauge_column"

			columnMapping := ColumnMapping{
				Name:  customColumnName,
				Usage: "GAUGE",
			}

			result := columnMapping.ToMetricMap(columnName, namespace, variableLabels)
			Expect(result[columnName].Desc.String()).To(Equal(prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, customColumnName),
				"", variableLabels, nil).String()))
		})
	})
})

var _ = Describe("UserQuery ToMetricMap", func() {
	var (
		namespace string
		userQuery UserQuery
	)

	BeforeEach(func() {
		namespace = "test_namespace"
		userQuery = UserQuery{
			Query:           "SELECT * FROM test",
			Metrics:         []Mapping{},
			Master:          true, // wokeignore:rule=master
			Primary:         true,
			CacheSeconds:    30,
			RunOnServer:     "testserver",
			TargetDatabases: []string{"test_db"},
		}
	})

	Context("when Metric Mapping has LABEL Usage", func() {
		BeforeEach(func() {
			userQuery.Metrics = []Mapping{
				{
					"label_column": ColumnMapping{
						Usage: "LABEL",
					},
				},
			}
		})

		It("should add label column to variable labels", func() {
			result, variableLabels := userQuery.ToMetricMap(namespace)

			Expect(variableLabels).To(ContainElement("label_column"))
			Expect(result["label_column"].Label).To(BeTrue())
		})
	})

	Context("when Metric Mapping has COUNTER Usage", func() {
		BeforeEach(func() {
			userQuery.Metrics = []Mapping{
				{
					"counter_column": ColumnMapping{
						Usage:       "COUNTER",
						Description: "Test counter",
					},
				},
			}
		})

		It("should add counter column to MetricMapSet with correct properties", func() {
			result, _ := userQuery.ToMetricMap(namespace)

			Expect(result).To(HaveKey("counter_column"))
			Expect(result["counter_column"].Desc.String()).To(Equal(prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, "counter_column"),
				"Test counter", []string{}, nil).String()))
			Expect(result["counter_column"].Vtype).To(Equal(prometheus.CounterValue))
		})
	})
})
