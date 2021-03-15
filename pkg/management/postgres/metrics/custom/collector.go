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
	"fmt"

	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// QueriesCollector is the implementation of PgCollector for a certain
// collection of custom queries supplied by the user
type QueriesCollector struct {
	instance      *postgres.Instance
	collectorName string

	userQueries    UserQueries
	mappings       map[string]MetricMapSet
	variableLabels map[string][]string
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
	result.variableLabels = make(map[string][]string)
	for name, queries := range result.userQueries {
		result.mappings[name], result.variableLabels[name] = queries.ToMetricMap(name)
	}

	return &result, nil
}

// QueryCollector is the implementation of PgCollector for a certain
// custom query supplied by the user
type QueryCollector struct {
	namespace      string
	userQuery      UserQuery
	columnMapping  map[string]MetricMap
	variableLabels []string
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
			log.Log.Info("Error while closing metrics extraction", "err", err.Error())
		}
		if rows.Err() != nil {
			log.Log.Info("Error while closing metrics extraction", "err", err.Error())
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

	for rows.Next() {
		if err = rows.Scan(scanArgs...); err != nil {
			return err
		}

		// This lookup map will be useful to find columns by name
		columnIdx := make(map[string]int)
		for idx, columnName := range columns {
			columnIdx[columnName] = idx
		}

		// Get the list of variable label names
		labels := make([]string, len(c.variableLabels))
		for idx, labelName := range c.variableLabels {
			labelIdx, ok := columnIdx[labelName]
			if ok {
				labels[idx], _ = DBToString(columnData[labelIdx])
			} else {
				log.Log.Info("Cannot find column for label",
					"labelName", labelName,
					"namespace", c.namespace)
			}
		}

		for idx, columnName := range columns {
			mapping, ok := c.columnMapping[columnName]

			switch {
			case ok && mapping.Discard:
				continue

			case ok && mapping.Histogram:
				c.collectHistogramMetric(mapping, columnName, columnIdx, columnData, labels, ch)

			case ok && !mapping.Histogram:
				c.collectConstMetric(mapping, columnData[idx], labels, ch)

			default:
				c.collectUnknownMetric(columnName, columnData[idx], labels, ch)
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

// collectUnknownMetric reports to the prometheus library an unknown metric
func (c QueryCollector) collectUnknownMetric(
	columnName string, value interface{}, variableLabels []string, ch chan<- prometheus.Metric) {
	// This metric is unknown, so let's pass it as untyped
	metricLabel := fmt.Sprintf("%s_%s", c.namespace, columnName)
	desc := prometheus.NewDesc(
		metricLabel,
		fmt.Sprintf("Unknown metric from %s", c.namespace), nil, nil)

	// Its not an error to fail here since we don't know how
	// to interpret this column
	floatData, ok := DBToFloat64(value)
	if !ok {
		log.Log.Info("Unparsable column type, discarding it",
			"namespace", c.namespace,
			"columnName", columnName,
			"value", value)
		return
	}

	metric := prometheus.MustNewConstMetric(desc, prometheus.UntypedValue, floatData, variableLabels...)
	ch <- metric
}

// collectConstMetric reports to the prometheus library a constant metric
func (c QueryCollector) collectConstMetric(
	mapping MetricMap, value interface{}, variableLabels []string, ch chan<- prometheus.Metric) {
	floatData, ok := DBToFloat64(value)
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
	columnName string,
	columnMap map[string]int,
	columnValues []interface{},
	variableLabels []string,
	ch chan<- prometheus.Metric) {
	keysIdx, ok := columnMap[columnName]
	if !ok {
		log.Log.Info("Cannot find histogram keys in query result",
			"namespace", c.namespace,
			"columnName", columnName,
			"columnMap", columnMap,
			"columnValues", columnValues)
		return
	}

	var keys []float64
	err := pq.Array(&keys).Scan(columnValues[keysIdx])
	if err != nil {
		log.Log.Info("Error while parsing bucket keys",
			"namespace", c.namespace,
			"value", columnValues[keysIdx],
			"mapping", mapping,
			"err", err.Error())
		return
	}

	var values []int64
	bucketsColumnName := columnName + "_bucket"
	valuesIdx, ok := columnMap[bucketsColumnName]
	if !ok {
		log.Log.Info("Cannot find histogram values in query result",
			"namespace", c.namespace,
			"columnName", bucketsColumnName,
			"columnMap", columnMap,
			"columnValues", columnValues)
		return
	}
	err = pq.Array(&values).Scan(columnValues[valuesIdx])
	if err != nil {
		log.Log.Info("Error while parsing bucket values",
			"namespace", c.namespace,
			"value", columnValues[valuesIdx],
			"mapping", mapping,
			"err", err.Error())
		return
	}

	buckets := make(map[float64]uint64, len(keys))
	for i, key := range keys {
		if i >= len(values) {
			break
		}
		buckets[key] = uint64(values[i])
	}

	sumColumnName := columnName + "_sum"
	sumIdx, ok := columnMap[sumColumnName]
	if !ok {
		log.Log.Info("Cannot find histogram sum in query result",
			"namespace", c.namespace,
			"columnName", sumColumnName,
			"columnMap", columnMap,
			"columnValues", columnValues)
		return
	}
	sum, ok := DBToFloat64(columnValues[sumIdx])
	if !ok {
		log.Log.Info("Error while parsing bucket sum",
			"namespace", c.namespace,
			"value", columnValues[valuesIdx],
			"mapping", mapping)
		return
	}

	countColumnName := columnName + "_count"
	countIdx, ok := columnMap[countColumnName]
	if !ok {
		log.Log.Info("Cannot find histogram count in query result",
			"namespace", c.namespace,
			"columnName", countColumnName,
			"columnMap", columnMap,
			"columnValues", columnValues)
		return
	}
	count, ok := DBToUint64(columnValues[countIdx])
	if !ok {
		log.Log.Info("Error while parsing bucket count",
			"namespace", c.namespace,
			"value", columnValues[valuesIdx],
			"mapping", mapping)
		return
	}

	metric := prometheus.MustNewConstHistogram(
		mapping.Desc,
		count, sum, buckets,
		variableLabels...,
	)
	ch <- metric
}
