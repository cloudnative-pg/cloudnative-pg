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

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
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
	Conversion func(interface{}) (float64, bool) `json:"-"`
}

// MetricMapSet is a set of MetricMap, usually associated to a UserQuery
type MetricMapSet []MetricMap

// VariableSet is a set of strings used as a collection of prometheus labels
type VariableSet []string

// ToMetricMap transform this user query in the metadata for a collection of Prometheus metrics,
// returning the metrics map and the list of variable labels
func (userQuery UserQuery) ToMetricMap(namespace string) (result MetricMapSet, variableLabels VariableSet) {
	for _, columnMapping := range userQuery.Metrics {
		// Create the list of variable labels
		for columnName, columnDescriptor := range columnMapping {
			if columnDescriptor.Usage == LABEL {
				variableLabels = append(variableLabels, columnName)
			}
		}

		// Create a mapping given the list of variable names
		for columnName, columnDescriptor := range columnMapping {
			result = append(
				result, columnDescriptor.ToMetricMap(columnName, namespace, variableLabels)...)
		}
	}

	return result, variableLabels
}

// ToMetricMap transform this query mapping in the metadata for a Prometheus metric. Since a query
// from the user can result in multiple metrics being generated (histograms are an example
// of this behavior), we are returning a mapping, which therefore should be collected together
func (columnMapping ColumnMapping) ToMetricMap(
	columnName, namespace string, variableLabels []string) (result MetricMapSet) {
	// Determine how to convert the column based on its usage.
	// nolint: dupl
	switch columnMapping.Usage {
	case DISCARD:
		result = append(result, MetricMap{
			Name:       columnName,
			Discard:    true,
			Conversion: nil,
			Label:      false,
		})

	case LABEL:
		result = append(result, MetricMap{
			Name:       columnName,
			Discard:    true,
			Conversion: nil,
			Label:      true,
		})

	case COUNTER:
		result = append(result, MetricMap{
			Name:  columnName,
			Vtype: prometheus.CounterValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: postgres.DBToFloat64,
			Label:      false,
		})

	case GAUGE:
		result = append(result, MetricMap{
			Name:  columnName,
			Vtype: prometheus.GaugeValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: postgres.DBToFloat64,
			Label:      false,
		})

	case HISTOGRAM:
		result = append(result, MetricMap{
			Name:      columnName,
			Histogram: true,
			Vtype:     prometheus.UntypedValue,
			Desc: prometheus.NewDesc(
				fmt.Sprintf("%s_%s", namespace, columnName),
				columnMapping.Description, variableLabels, nil),
			Conversion: postgres.DBToFloat64,
			Label:      false,
		})
		result = append(result, MetricMap{
			Name:      columnName + "_bucket",
			Histogram: true,
			Discard:   true,
			Label:     false,
		})
		result = append(result, MetricMap{
			Name:      columnName + "_sum",
			Histogram: true,
			Discard:   true,
			Label:     false,
		})
		result = append(result, MetricMap{
			Name:      columnName + "_count",
			Histogram: true,
			Discard:   true,
			Label:     false,
		})

	case MAPPEDMETRIC:
		result = append(result, MetricMap{
			Name:  columnName,
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
			Label: false,
		})

	case DURATION:
		result = append(result, MetricMap{
			Name:  columnName,
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
			Label: false,
		})
	}

	return result
}
