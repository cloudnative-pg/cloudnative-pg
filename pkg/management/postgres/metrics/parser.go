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

// Package metrics enables to expose a set of metrics and collectors on a given postgres instance
package metrics

import (
	"database/sql"
	"errors"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// UserQueries is a collection of custom queries
type UserQueries map[string]UserQuery

// UserQuery represent a query created by the user
type UserQuery struct {
	Query           string    `yaml:"query"`
	PredicateQuery  string    `yaml:"predicate_query"`
	Metrics         []Mapping `yaml:"metrics"`
	Master          bool      `yaml:"master"` // wokeignore:rule=master
	Primary         bool      `yaml:"primary"`
	CacheSeconds    uint64    `yaml:"cache_seconds"`
	RunOnServer     string    `yaml:"runonserver"`
	TargetDatabases []string  `yaml:"target_databases"`
	// Name allows overriding the key name in the metric namespace
	Name string `yaml:"name"`
}

// Mapping decide how a certain field, extracted from the query's result, should be used
type Mapping map[string]ColumnMapping

// ColumnMapping is a representation of a prometheus descriptor map
type ColumnMapping struct {
	Usage       ColumnUsage `yaml:"usage"`
	Description string      `yaml:"description"`

	// Mapping is an optional column mapping for MAPPEDMETRIC
	Mapping map[string]float64 `yaml:"metric_mapping"`

	// SupportedVersions are the semantic version ranges which are supported.
	SupportedVersions string `yaml:"pg_version"`

	// Name allows overriding the key name when naming the column
	Name string `yaml:"name"`
}

// ColumnUsage represent how a certain column should be used
type ColumnUsage string

const (
	// DISCARD means that this column should be ignored
	DISCARD ColumnUsage = "DISCARD"

	// LABEL means use this column as a label
	LABEL ColumnUsage = "LABEL"

	// COUNTER means use this column as a counter
	COUNTER ColumnUsage = "COUNTER"

	// GAUGE means use this column as a gauge
	GAUGE ColumnUsage = "GAUGE"

	// MAPPEDMETRIC means use this column with the supplied mapping of text values
	MAPPEDMETRIC ColumnUsage = "MAPPEDMETRIC" // Use this column with the supplied mapping of text values

	// DURATION means use this column as a text duration (in milliseconds)
	DURATION ColumnUsage = "DURATION"

	// HISTOGRAM means use this column as an histogram
	HISTOGRAM ColumnUsage = "HISTOGRAM"
)

// ParseQueries parse a YAML file containing custom queries
func ParseQueries(content []byte) (UserQueries, error) {
	var result UserQueries

	if err := yaml.Unmarshal(content, &result); err != nil {
		return nil, fmt.Errorf("parsing user queries: %w", err)
	}

	return result, nil
}

// isCollectable checks if a query to collect metrics should be executed.
// The method tests the query provided in the PredicateQuery property within the same transaction
// used to collect metrics.
// PredicateQuery should return at most a single row with a single column with type bool.
// If no PredicateQuery is provided, the query is considered collectable by default
func (userQuery UserQuery) isCollectable(tx *sql.Tx) (bool, error) {
	if userQuery.PredicateQuery == "" {
		return true, nil
	}

	var isCollectable sql.NullBool
	if err := tx.QueryRow(userQuery.PredicateQuery).Scan(&isCollectable); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	if !isCollectable.Valid {
		return false, nil
	}

	return isCollectable.Bool, nil
}
