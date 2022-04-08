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

// Package metrics enables to expose a set of metrics and collectors on a given postgres instance
package metrics

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// UserQueries is a collection of custom queries
type UserQueries map[string]UserQuery

// UserQuery represent a query created by the user
type UserQuery struct {
	Query           string    `yaml:"query"`
	Metrics         []Mapping `yaml:"metrics"`
	Master          bool      `yaml:"master"` // wokeignore:rule=master
	Primary         bool      `yaml:"primary"`
	CacheSeconds    uint64    `yaml:"cache_seconds"`
	RunOnServer     string    `yaml:"runonserver"`
	TargetDatabases []string  `yaml:"target_databases"`
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
