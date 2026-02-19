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

// This code is inspired on [postgres_exporter](https://github.com/prometheus-community/postgres_exporter)

import (
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
)

// MetricMap stores the prometheus metric description which a given column will
// be mapped to by the collector
type MetricMap struct {
	// The name of this metric
	Name string

	// Is this metric a label?
	Label bool

	// Should metric be discarded during metrics generation?
	// Labels should be discarded because they are used while generating
	// names and not values
	Discard bool

	// Should metric be treated as a histogram?
	Histogram bool

	// Vtype is the prometheus valueType
	Vtype prometheus.ValueType

	// Desc is the prometheus descriptor
	Desc *prometheus.Desc

	// Conversion is the function to convert a dynamic type into a value suitable
	// for the exporter
	Conversion func(any) (float64, bool) `json:"-"`
}

// MetricMapSet is a set of MetricMap, usually associated to a UserQuery
type MetricMapSet map[string]MetricMap

// VariableSet is a set of strings used as a collection of prometheus labels
type VariableSet []string

// ToMetricMap transform this user query in the metadata for a collection of Prometheus metrics,
// returning the metrics map and the list of variable labels
func (userQuery UserQuery) ToMetricMap(namespace string) (result MetricMapSet, variableLabels VariableSet) {
	// Create the list of variable labels
	for _, columnMapping := range userQuery.Metrics {
		for columnName, columnDescriptor := range columnMapping {
			if columnDescriptor.Usage == LABEL {
				variableLabels = append(variableLabels, columnName)
			}
		}
	}

	result = make(MetricMapSet)
	for _, columnMapping := range userQuery.Metrics {
		// Create a mapping given the list of variable names
		for columnName, columnDescriptor := range columnMapping {
			for k, v := range columnDescriptor.ToMetricMap(columnName, namespace, variableLabels) {
				result[k] = v
			}
		}
	}

	return result, variableLabels
}

// ToMetricMap transform this query mapping in the metadata for a Prometheus metric. Since a query
// from the user can result in multiple metrics being generated (histograms are an example
// of this behavior), we are returning a mapping, which therefore should be collected together
func (columnMapping ColumnMapping) ToMetricMap(
	columnName, namespace string, variableLabels []string,
) MetricMapSet {
	result := make(MetricMapSet)
	columnFQName := fmt.Sprintf("%s_%s", namespace, columnName)
	if columnMapping.Name != "" {
		columnFQName = fmt.Sprintf("%s_%s", namespace, columnMapping.Name)
	}
	// Determine how to convert the column based on its usage.
	// nolint: dupl
	switch columnMapping.Usage {
	case DISCARD:
		result[columnName] = MetricMap{
			Name:       columnName,
			Discard:    true,
			Conversion: nil,
			Label:      false,
		}

	case LABEL:
		result[columnName] = MetricMap{
			Name:       columnName,
			Discard:    true,
			Conversion: nil,
			Label:      true,
		}

	case COUNTER:
		result[columnName] = MetricMap{
			Name:  columnName,
			Vtype: prometheus.CounterValue,
			Desc: prometheus.NewDesc(
				columnFQName,
				columnMapping.Description, variableLabels, nil),
			Conversion: postgresutils.DBToFloat64,
			Label:      false,
		}

	case GAUGE:
		result[columnName] = MetricMap{
			Name:  columnName,
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				columnFQName,
				columnMapping.Description, variableLabels, nil),
			Conversion: postgresutils.DBToFloat64,
			Label:      false,
		}

	case HISTOGRAM:
		result[columnName] = MetricMap{
			Name:      columnName,
			Histogram: true,
			Vtype:     prometheus.UntypedValue,
			Desc: prometheus.NewDesc(
				columnFQName,
				columnMapping.Description, variableLabels, nil),
			Conversion: postgresutils.DBToFloat64,
			Label:      false,
		}
		bucketColumnName := columnName + "_bucket"
		result[bucketColumnName] = MetricMap{
			Name:      bucketColumnName,
			Histogram: true,
			Discard:   true,
			Label:     false,
		}
		sumColumnName := columnName + "_sum"
		result[sumColumnName] = MetricMap{
			Name:      sumColumnName,
			Histogram: true,
			Discard:   true,
			Label:     false,
		}
		countColumnName := columnName + "_count"
		result[countColumnName] = MetricMap{
			Name:      countColumnName,
			Histogram: true,
			Discard:   true,
			Label:     false,
		}

	case MAPPEDMETRIC:
		result[columnName] = MetricMap{
			Name:  columnName,
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				columnFQName,
				columnMapping.Description, variableLabels, nil),
			Conversion: func(in any) (float64, bool) {
				text, ok := in.(string)
				if !ok {
					return math.NaN(), false
				}

				val, ok := columnMapping.Mapping[text]
				if !ok {
					return math.NaN(), false
				}
				return val, true
			},
			Label: false,
		}

	case DURATION:
		result[columnName] = MetricMap{
			Name:  columnName,
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_milliseconds", columnFQName),
				columnMapping.Description, variableLabels, nil),
			Conversion: func(in any) (float64, bool) {
				var durationString string
				switch t := in.(type) {
				case []byte:
					durationString = string(t)
				case string:
					durationString = t
				default:
					return math.NaN(), false
				}

				if durationString == "-1" {
					return math.NaN(), false
				}

				d, err := time.ParseDuration(durationString)
				if err != nil {
					return math.NaN(), false
				}
				return float64(d / time.Millisecond), true
			},
			Label: false,
		}
	}

	return result
}
