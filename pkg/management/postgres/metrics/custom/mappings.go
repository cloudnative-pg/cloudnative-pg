/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package custom

// This code is heavily based on [pg_exporter](https://github.com/prometheus-community/postgres_exporter)
// since we are reusing the custom query infrastructure that that project already have.

import (
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricMap stores the prometheus metric description which a given column will
// be mapped to by the collector
type MetricMap struct {
	// Should metric be discarded during mapping?
	Discard bool

	// Should metric be treated as a histogram?
	Histogram bool

	// Vtype is the prometheus valueType
	Vtype prometheus.ValueType

	// Desc is the prometheus descriptor
	Desc *prometheus.Desc

	// Conversion is the function to convert a dynamic type into a value suitable
	// for the exporter
	Conversion func(interface{}) (float64, bool) `json:"-"`
}

// MetricMapSet is a set of MetricMap, usually associated to a UserQuery
type MetricMapSet map[string]MetricMap

// ToMetricMap transform this user query in the metadata for a collection of Prometheus metrics,
// returning the metrics map and the list of variable labels
func (userQuery UserQuery) ToMetricMap(namespace string) (MetricMapSet, []string) {
	result := make(map[string]MetricMap)
	var variableLabels []string

	for _, columnMapping := range userQuery.Metrics {
		// Create the list of variable labels
		for columnName, columnDescriptor := range columnMapping {
			if columnDescriptor.Usage == LABEL {
				variableLabels = append(variableLabels, columnName)
			}
		}

		// Create a mapping given the list of variable names
		for columnName, columnDescriptor := range columnMapping {
			intermediate := columnDescriptor.ToMetricMap(columnName, namespace, variableLabels)
			for mappingName, mapping := range intermediate {
				result[mappingName] = mapping
			}
		}
	}

	return result, variableLabels
}

// ToMetricMap transform this query mapping in the metadata for a Prometheus metric. Since a query
// from the user can result in multiple metrics being generated (histograms are an example
// of this behavior) we are returning a mapping, which should then be collected together
func (columnMapping ColumnMapping) ToMetricMap(columnName, namespace string, variableLabels []string) MetricMapSet {
	result := make(map[string]MetricMap)

	// Determine how to convert the column based on its usage.
	// nolint: dupl
	switch columnMapping.Usage {
	case DISCARD, LABEL:
		result[columnName] = MetricMap{
			Discard:    true,
			Conversion: nil,
		}

	case COUNTER:
		result[columnName] = MetricMap{
			Vtype: prometheus.CounterValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: DBToFloat64,
		}

	case GAUGE:
		result[columnName] = MetricMap{
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: DBToFloat64,
		}

	case HISTOGRAM:
		result[columnName] = MetricMap{
			Histogram: true,
			Vtype:     prometheus.UntypedValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: DBToFloat64,
		}
		result[columnName+"_bucket"] = MetricMap{
			Histogram: true,
			Discard:   true,
		}
		result[columnName+"_sum"] = MetricMap{
			Histogram: true,
			Discard:   true,
		}
		result[columnName+"_count"] = MetricMap{
			Histogram: true,
			Discard:   true,
		}

	case MAPPEDMETRIC:
		result[columnName] = MetricMap{
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: func(in interface{}) (float64, bool) {
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
		}

	case DURATION:
		result[columnName] = MetricMap{
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s_milliseconds", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: func(in interface{}) (float64, bool) {
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
		}
	}

	return result
}
