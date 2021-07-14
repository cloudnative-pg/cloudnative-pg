/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// This code is inspired on [postgres_exporter](https://github.com/prometheus-community/postgres_exporter)

package metrics

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics/histogram"
)

// QueriesCollector is the implementation of PgCollector for a certain
// collection of custom queries supplied by the user
type QueriesCollector struct {
	instance      *postgres.Instance
	defaultDBName string
	collectorName string

	userQueries    UserQueries
	mappings       map[string]MetricMapSet
	variableLabels map[string]VariableSet

	errorUserQueries      *prometheus.CounterVec
	errorUserQueriesGauge prometheus.Gauge
}

// Name returns the name of this collector, as supplied by the user in the configMap
func (q QueriesCollector) Name() string {
	return q.collectorName
}

// Collect load data from the actual PostgreSQL instance
func (q QueriesCollector) Collect(ch chan<- prometheus.Metric) error {
	isPrimary, err := q.instance.IsPrimary()
	if err != nil {
		return err
	}

	// reset before collecting
	q.errorUserQueries.Reset()

	for name, userQuery := range q.userQueries {
		collector := QueryCollector{
			namespace:      name,
			userQuery:      userQuery,
			columnMapping:  q.mappings[name],
			variableLabels: q.variableLabels[name],
		}

		if (userQuery.Primary || userQuery.Master) && !isPrimary { // wokeignore:rule=master
			log.Log.V(1).Info("Skipping because runs only on primary", "query", name)
			continue
		}

		log.Log.V(1).Info("Collecting data", "query", name)

		targetDatabases := userQuery.TargetDatabases
		if len(targetDatabases) == 0 {
			targetDatabases = append(targetDatabases, q.defaultDBName)
		}
		for _, targetDatabase := range targetDatabases {
			conn, err := q.instance.ConnectionPool().Connection(targetDatabase)
			if err != nil {
				return err
			}
			err = collector.collect(conn, ch)
			if err != nil {
				log.Log.Error(err, "Error collecting user query", "query name", name,
					"targetDatabase", targetDatabase)
				// increment metrics counters.
				q.errorUserQueries.WithLabelValues(name + ": " + err.Error()).Inc()
				q.errorUserQueriesGauge.Set(1)
			}
		}
	}

	// add err it into errorUserQueriesVec and errorUserQueriesGauge metrics.
	q.errorUserQueriesGauge.Collect(ch)
	q.errorUserQueries.Collect(ch)

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

	// add error user queries description
	q.errorUserQueries.Describe(ch)
	q.errorUserQueriesGauge.Describe(ch)
}

// NewQueriesCollector create a new PgCollector working over a set of custom queries
// supplied by the user
func NewQueriesCollector(name string, instance *postgres.Instance, defaultDBName string) *QueriesCollector {
	return &QueriesCollector{
		collectorName:  name,
		instance:       instance,
		mappings:       make(map[string]MetricMapSet),
		variableLabels: make(map[string]VariableSet),
		userQueries:    make(UserQueries),
		defaultDBName:  defaultDBName,
		errorUserQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: name,
			Name:      "errors_total",
			Help:      "Total errors occurred performing user queries.",
		}, []string{"errorUserQueries"}),
		errorUserQueriesGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: name,
			Name:      "last_error",
			Help:      "1 if the last collection ended with error, 0 otherwise.",
		}),
	}
}

// ParseQueries parse a YAML file containing custom queries and add it
// to the set of gathered one
func (q *QueriesCollector) ParseQueries(customQueries []byte) error {
	var err error

	parsedQueries, err := ParseQueries(customQueries)
	if err != nil {
		return err
	}
	for name, query := range parsedQueries {
		q.userQueries[name] = query
		q.mappings[name], q.variableLabels[name] = query.ToMetricMap(
			fmt.Sprintf("%v_%v", q.collectorName, name))
	}

	return nil
}

// QueryCollector is the implementation of PgCollector for a certain
// custom query supplied by the user
type QueryCollector struct {
	namespace      string
	userQuery      UserQuery
	columnMapping  MetricMapSet
	variableLabels VariableSet
}

// collect collects the result from query and writes it to the prometheus
// infrastructure
func (c QueryCollector) collect(conn *sql.DB, ch chan<- prometheus.Metric) error {
	tx, err := createMonitoringTx(conn)
	if err != nil {
		return err
	}

	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Log.Info("Error while rolling back metrics extraction", "err", err.Error())
		}
	}()

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

		labels, done := c.collectLabels(columns, columnData)
		if done {
			c.collectColumns(columns, columnData, labels, ch)
		}
	}
	return nil
}

// Collect the list of labels from the database, and returns true if the
// label extraction succeeded, false otherwise
func (c QueryCollector) collectLabels(columns []string, columnData []interface{}) ([]string, bool) {
	var labels []string
	for idx, columnName := range columns {
		if mapping, ok := c.columnMapping[columnName]; ok && mapping.Label {
			value, ok := postgres.DBToString(columnData[idx])
			if !ok {
				log.Log.Info("Label value cannot be converted to string",
					"value", value,
					"mapping", mapping)
				return nil, false
			}

			labels = append(labels, value)
		}
	}
	return labels, true
}

// Collect the metrics from the database columns
func (c QueryCollector) collectColumns(columns []string, columnData []interface{},
	labels []string, ch chan<- prometheus.Metric) {
	for idx, columnName := range columns {
		mapping, ok := c.columnMapping[columnName]
		if !ok {
			log.Log.Info("Missing mapping for column", "column", columnName, "mapping", c.columnMapping)
			continue
		}

		// There is a strong difference between histogram and non-histogram metrics in
		// postgres_exporter. The first ones are looked up by column name and the second
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
					"columnName", columnName,
					"mapping.Name", mapping.Name,
					"mappings", c.columnMapping,
					"mapping", mapping,
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

// createMonitoringTx create a monitoring transaction with read-only access
// and role set to `pg_monitor`
func createMonitoringTx(conn *sql.DB) (*sql.Tx, error) {
	ctx := context.Background()
	defer ctx.Done()

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{
		ReadOnly: true,
	})
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("SET application_name TO cnp_metrics_exporter")
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("SET ROLE TO pg_monitor")

	return tx, err
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
