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
	"path"
	"regexp"

	"github.com/blang/semver"
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
	pgVersion     *semver.Version

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

var isPathPattern = regexp.MustCompile(`[][*?]`)

// Collect load data from the actual PostgreSQL instance
func (q QueriesCollector) Collect(ch chan<- prometheus.Metric) error {
	// Reset before collecting
	q.errorUserQueries.Reset()

	err := q.collectUserQueries(ch)
	if err != nil {
		return err
	}

	// Add errors into errorUserQueriesVec and errorUserQueriesGauge metrics
	q.errorUserQueriesGauge.Collect(ch)
	q.errorUserQueries.Collect(ch)

	return nil
}

func (q QueriesCollector) collectUserQueries(ch chan<- prometheus.Metric) error {
	isPrimary, err := q.instance.IsPrimary()
	if err != nil {
		return err
	}

	// In case more than one user query specify a pattern in target_databases,
	// we need to get them just once
	var allAccessibleDatabasesCache []string

	for name, userQuery := range q.userQueries {
		queryLogger := log.WithValues("query", name)
		collector := QueryCollector{
			namespace:      name,
			userQuery:      userQuery,
			columnMapping:  q.mappings[name],
			variableLabels: q.variableLabels[name],
		}

		if !q.toBeChecked(name, userQuery, isPrimary, queryLogger) {
			continue
		}

		queryLogger.Debug("Collecting data")

		targetDatabases := userQuery.TargetDatabases
		if len(targetDatabases) == 0 {
			targetDatabases = append(targetDatabases, q.defaultDBName)
		}

		// Initialize the cache is one of the target contains a pattern
		if allAccessibleDatabasesCache == nil {
			for _, targetDatabase := range targetDatabases {
				if isPathPattern.MatchString(targetDatabase) {
					databases, err := q.getAllAccessibleDatabases()
					if err != nil {
						q.reportUserQueryErrorMetric(name + ": " + err.Error())
						break
					}
					allAccessibleDatabasesCache = databases
					break
				}
			}
		}

		allTargetDatabases := q.expandTargetDatabases(targetDatabases, allAccessibleDatabasesCache)
		for targetDatabase := range allTargetDatabases {
			conn, err := q.instance.ConnectionPool().Connection(targetDatabase)
			if err != nil {
				q.reportUserQueryErrorMetric(name + ": " + err.Error())
				continue
			}
			err = collector.collect(conn, ch)
			if err != nil {
				queryLogger.Error(err, "Error collecting user query",
					"targetDatabase", targetDatabase)
				// Increment metrics counters.
				q.reportUserQueryErrorMetric(name + " on db " + targetDatabase + ": " + err.Error())
			}
		}
	}
	return nil
}

func (q QueriesCollector) toBeChecked(name string, userQuery UserQuery, isPrimary bool, queryLogger log.Logger) bool {
	if (userQuery.Primary || userQuery.Master) && !isPrimary { // wokeignore:rule=master
		queryLogger.Info("Skipping because runs only on primary")
		return false
	}

	if runOnServer := userQuery.RunOnServer; runOnServer != "" {
		matchesVersion, err := q.checkRunOnServerMatches(runOnServer, name)
		// any error should result in the query not being executed
		if err != nil {
			queryLogger.Error(err, "while checking runOnServer version matches",
				"runOnServer", runOnServer)
			q.reportUserQueryErrorMetric(name)
			return false
		} else if !matchesVersion {
			queryLogger.Info("Skipping because runs only on other postgres versions",
				"runOnServer", runOnServer)
			return false
		}
	}

	return true
}

func (q QueriesCollector) reportUserQueryErrorMetric(label string) {
	q.errorUserQueries.WithLabelValues(label).Inc()
	q.errorUserQueriesGauge.Set(1)
}

// GetPgVersion queries the postgres instance to know the current version, parses it and memoize it for future uses
func (q QueriesCollector) GetPgVersion() (*semver.Version, error) {
	if q.pgVersion != nil {
		return q.pgVersion, nil
	}
	db, err := q.instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}
	var versionString string
	row := db.QueryRow("SHOW server_version;")
	err = row.Scan(&versionString)
	if err != nil {
		return nil, err
	}
	parsedVersion, err := semver.ParseTolerant(versionString)
	if err != nil {
		return nil, err
	}
	q.pgVersion = &parsedVersion

	return q.pgVersion, nil
}

func (q QueriesCollector) checkRunOnServerMatches(runOnServer string, name string) (bool, error) {
	isVersionInRange, err := semver.ParseRange(runOnServer)
	if err != nil {
		log.Error(err, "while parsing runOnServer version range",
			"runOnServer", runOnServer, "query", name)
		return false, err
	}
	q.pgVersion, err = q.GetPgVersion()
	if err != nil {
		log.Error(err, "while getting current pgVersion",
			"runOnServer", runOnServer, "query", name)
		return false, err
	}
	return isVersionInRange(*q.pgVersion), nil
}

func (q QueriesCollector) expandTargetDatabases(
	targetDatabases []string,
	allAccessibleDatabasesCache []string,
) (allTargetDatabases map[string]bool) {
	allTargetDatabases = make(map[string]bool)
	for _, targetDatabase := range targetDatabases {
		if !isPathPattern.MatchString(targetDatabase) {
			allTargetDatabases[targetDatabase] = true
			continue
		}
		for _, database := range allAccessibleDatabasesCache {
			matched, err := path.Match(targetDatabase, database)
			if err == nil && matched {
				allTargetDatabases[database] = true
			}
		}
	}
	return allTargetDatabases
}

func (q QueriesCollector) getAllAccessibleDatabases() ([]string, error) {
	conn, err := q.instance.ConnectionPool().Connection(q.defaultDBName)
	if err != nil {
		return nil, fmt.Errorf("while connecting to expand target_database *: %w", err)
	}
	tx, err := createMonitoringTx(conn)
	if err != nil {
		return nil, fmt.Errorf("while creating monitoring tx to retrieve accessible databases list: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Info("Error while rolling back monitoring tx to retrieve accessible databases list", "err", err.Error())
		}
	}()
	databases, errors := postgres.GetAllAccessibleDatabases(tx, "datallowconn AND NOT datistemplate")
	if errors != nil {
		return nil, fmt.Errorf("while discovering databases for metrics: %v", errors)
	}
	return databases, nil
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
			log.Info("Error while rolling back metrics extraction", "err", err.Error())
		}
	}()

	rows, err := tx.Query(c.userQuery.Query)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Info("Error while closing metrics extraction",
				"err", err.Error())
		}
		if rows.Err() != nil {
			log.Info("Error while loading metrics",
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
		log.Info("Columns number mismatch",
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
				log.Info("Label value cannot be converted to string",
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
			log.Info("Missing mapping for column", "column", columnName, "mapping", c.columnMapping)
			continue
		}

		// There is a strong difference between histogram and non-histogram metrics in
		// postgres_exporter. The first ones are looked up by column name and the second
		// ones are looked up just using the index.
		//
		// We implemented the same behavior here.

		switch {
		case mapping.Discard || mapping.Label:
			continue

		case mapping.Histogram:
			histogramData, err := histogram.NewFromRawData(columnData, columns, columnName)
			if err != nil {
				log.Error(err, "Cannot process histogram metric",
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

		default:
			c.collectConstMetric(mapping, columnData[idx], labels, ch)
		}
	}
}

// createMonitoringTx create a monitoring transaction with read-only access
// and role set to `pg_monitor`
func createMonitoringTx(conn *sql.DB) (*sql.Tx, error) {
	tx, err := conn.BeginTx(context.Background(), &sql.TxOptions{
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
		log.Info("Error while parsing value",
			"namespace", c.namespace,
			"value", value,
			"mapping", mapping)
		return
	}

	// Generate the metric
	metric, err := prometheus.NewConstMetric(mapping.Desc, mapping.Vtype, floatData, variableLabels...)
	if err != nil {
		log.Error(err, "while collecting constant metric", "metric", mapping.Name)
		return
	}
	ch <- metric
}

// collectHistogramMetric reports to the prometheus library an histogram-based metric
func (c QueryCollector) collectHistogramMetric(
	mapping MetricMap,
	columnData *histogram.Value,
	variableLabels []string,
	ch chan<- prometheus.Metric) {
	metric, err := prometheus.NewConstHistogram(
		mapping.Desc,
		columnData.Count, columnData.Sum, columnData.Buckets,
		variableLabels...,
	)
	if err != nil {
		log.Error(err, "while collecting histogram metric", "metric", mapping.Name)
		return
	}
	ch <- metric
}
