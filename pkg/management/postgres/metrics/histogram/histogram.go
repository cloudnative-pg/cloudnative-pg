/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// This code is inspired on [postgres_exporter](https://github.com/prometheus-community/postgres_exporter)

// Package histogram contain histogram-metrics related functions
package histogram

import (
	"fmt"

	"github.com/lib/pq"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

const (
	sumSuffix    = "_sum"
	countSuffix  = "_count"
	bucketSuffix = "_bucket"
)

// Value represent the series of values extracted by
// a PostgreSQL SQL query to create a Prometheus histogram metric
type Value struct {
	Keys   []float64
	Values []int64
	Sum    float64
	Count  uint64

	Buckets map[float64]uint64
}

// NewFromRawData load data from the raw database row into
// an histogram value
func NewFromRawData(values []interface{}, columns []string, name string) (*Value, error) {
	var ok bool

	histogramValue := &Value{}

	for idx, columnName := range columns {
		switch columnName {
		case name:
			err := pq.Array(&histogramValue.Keys).Scan(values[idx])
			if err != nil {
				return nil, fmt.Errorf("cannot load histogram values: %w", err)
			}
		case name + sumSuffix:
			histogramValue.Sum, ok = postgres.DBToFloat64(values[idx])
			if !ok {
				return nil, fmt.Errorf("cannot convert histogram values")
			}

		case name + countSuffix:
			histogramValue.Count, ok = postgres.DBToUint64(values[idx])
			if !ok {
				return nil, fmt.Errorf("cannot convertg histogram depth")
			}

		case name + bucketSuffix:
			err := pq.Array(&histogramValue.Values).Scan(values[idx])
			if err != nil {
				return nil, fmt.Errorf("cannot load histogram keys: %w", err)
			}
		}
	}

	if histogramValue.Keys == nil {
		return nil, fmt.Errorf("histogram keys missing")
	}

	if histogramValue.Values == nil {
		return nil, fmt.Errorf("histogram values missing")
	}

	histogramValue.Buckets = make(map[float64]uint64, len(histogramValue.Keys))
	for i, key := range histogramValue.Keys {
		if i >= len(histogramValue.Values) {
			break
		}
		histogramValue.Buckets[key] = uint64(histogramValue.Values[i])
	}

	return histogramValue, nil
}
