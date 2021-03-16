/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// This code is heavily based on [pg_exporter](https://github.com/prometheus-community/postgres_exporter)
// since we are reusing the custom query infrastructure that that project already have.

package custom

import (
	"context"
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics/histogram"
)

// QueriesCollector is the implementation of PgCollector for a certain
// collection of custom queries supplied by the user
type QueriesCollector struct {
	instance      *postgres.Instance
	collectorName string

	userQueries    UserQueries
	mappings       map[string]MetricMapSet
	variableLabels map[string]VariableSet
}

// Name returns the name of this collector, as supplied by the user in the configMap
func (q QueriesCollector) Name() string {
	return q.collectorName
}

// Collect load data from the actual PostgreSQL instance
func (q QueriesCollector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()

	isPrimary, err := q.instance.IsPrimary()
	if err != nil {
		return err
	}

	conn, err := q.instance.GetApplicationDB()
	if err != nil {
		return err
	}

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{
		ReadOnly: true,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Log.Info("Error while rolling back metrics extraction", "err", err.Error())
		}
	}()

	_, err = tx.Exec("SET ROLE TO pg_monitor")
	if err != nil {
		return err
	}

	for name, userQuery := range q.userQueries {
		collector := QueryCollector{
			namespace:      name,
			userQuery:      userQuery,
			columnMapping:  q.mappings[name],
			variableLabels: q.variableLabels[name],
		}

		if (userQuery.Primary || userQuery.Master) && !isPrimary { // wokeignore:rule=master
			continue
		}

		err := collector.collect(tx, ch)
		if err != nil {
			return err
		}
	}

	return nil
}

// Describe implements the prometheus.Collector and defines the metrics with return
func (q QueriesCollector) Describe(ch chan<- *prometheus.Desc) {
	for name, userQuery := range q.userQueries {
		collector := QueryCollector{
			namespace:     name,
			userQuery:     userQuery,
			columnMapping: q.mappings[name],
		}

		collector.describe(ch)
	}
}

// NewQueriesCollector create a new PgCollector working over a set of custom queries
// supplied by the user
func NewQueriesCollector(name string, instance *postgres.Instance, customQueries []byte) (*QueriesCollector, error) {
	result := QueriesCollector{
		collectorName: name,
		instance:      instance,
	}

	var err error
	result.userQueries, err = ParseQueries(customQueries)
	if err != nil {
		return nil, err
	}

	result.mappings = make(map[string]MetricMapSet)
	result.variableLabels = make(map[string]VariableSet)
	for queryName, queries := range result.userQueries {
		result.mappings[queryName], result.variableLabels[queryName] = queries.ToMetricMap(queryName)
	}

	return &result, nil
}

// QueryCollector is the implementation of PgCollector for a certain
// custom query supplied by the user
type QueryCollector struct {
	namespace      string
	userQuery      UserQuery
	columnMapping  MetricMapSet
	variableLabels VariableSet
}

// collect collect the result from query and writes it to the prometheus
// infrastructure
func (c QueryCollector) collect(tx *sql.Tx, ch chan<- prometheus.Metric) error {
	rows, err := tx.Query(c.userQuery.Query)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Log.Info("Error while closing metrics extraction",
				"err", err.Error())
		}
		if rows.Err() != nil {
			log.Log.Info("Error while loading metrics",
				"err", err.Error())
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	columnData := make([]interface{}, len(columns))
	scanArgs := make([]interface{}, len(columns))
	for i := range columnData {
		scanArgs[i] = &columnData[i]
	}

	if len(columns) != len(c.columnMapping) {
		log.Log.Info("Columns number mismatch",
			"name", c.namespace,
			"columnNumberFromDB", len(columns),
			"columnNumberFromConfiguration", len(c.columnMapping))
		return nil
	}

	for rows.Next() {
		if err = rows.Scan(scanArgs...); err != nil {
			return err
		}

		// Get the list of variable label names
		var labels []string
		for idx, mapping := range c.columnMapping {
			if mapping.Label {
				value, ok := postgres.DBToString(columnData[idx])
				if !ok {
					log.Log.Info("Label value cannot be converted to string",
						"value", value,
						"mapping", mapping)
					return nil
				}

				labels = append(labels, value)
			}
		}

		for idx, columnName := range columns {
			mapping := c.columnMapping[idx]

			// There is a strong difference between histogram and non-histogram metrics in
			// pg_exporter. The first ones are looked up by column name and the second
			// ones are looked up just using the index.
			//
			// We implemented the same behavior here.

			switch {
			case mapping.Discard:
				continue

			case mapping.Histogram:
				histogramData, err := histogram.NewFromRawData(columnData, columns, columnName)
				if err != nil {
					log.Log.Error(err, "Cannot process histogram metric",
						"columns", columns,
						"columnData", columnData,
						"labels", labels)
				} else {
					c.collectHistogramMetric(mapping, histogramData, labels, ch)
				}

			case !mapping.Histogram:
				c.collectConstMetric(mapping, columnData[idx], labels, ch)
			}
		}
	}

	return nil
}

// describe puts in the channel the metadata we have for the queries we collect
func (c QueryCollector) describe(ch chan<- *prometheus.Desc) {
	for _, mapSet := range c.columnMapping {
		ch <- mapSet.Desc
	}
}

// collectConstMetric reports to the prometheus library a constant metric
func (c QueryCollector) collectConstMetric(
	mapping MetricMap, value interface{}, variableLabels []string, ch chan<- prometheus.Metric) {
	floatData, ok := postgres.DBToFloat64(value)
	if !ok {
		log.Log.Info("Error while parsing value",
			"namespace", c.namespace,
			"value", value,
			"mapping", mapping)
		return
	}

	// Generate the metric
	metric := prometheus.MustNewConstMetric(mapping.Desc, mapping.Vtype, floatData, variableLabels...)
	ch <- metric
}

// collectHistogramMetric reports to the prometheus library an histogram-based metric
func (c QueryCollector) collectHistogramMetric(
	mapping MetricMap,
	columnData *histogram.Value,
	variableLabels []string,
	ch chan<- prometheus.Metric) {
	metric := prometheus.MustNewConstHistogram(
		mapping.Desc,
		columnData.Count, columnData.Sum, columnData.Buckets,
		variableLabels...,
	)
	ch <- metric
}
