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

// This code is inspired on [postgres_exporter](https://github.com/prometheus-community/postgres_exporter)

package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/prometheus/client_golang/prometheus"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/metrics/histogram"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
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
	lastUpdateTimestamp   prometheus.Gauge
	cacheHits             prometheus.Gauge
	cacheMiss             prometheus.Gauge

	computedMetrics []prometheus.Metric
	timeLastUpdated time.Time

	// metricsMutex guards access to computedMetrics, error metrics, and timestamp
	// of last update,
	// to avoid races between Update (writer) and Collect (reader)
	metricsMutex sync.RWMutex
}

// Name returns the name of this collector, as supplied by the user in the configMap
func (q *QueriesCollector) Name() string {
	return q.collectorName
}

var isPathPattern = regexp.MustCompile(`[][*?]`)

// Update recomputes the metrics from the user queries
func (q *QueriesCollector) Update() error {
	q.metricsMutex.Lock()
	defer q.metricsMutex.Unlock()

	// Start a fresh error state for this cycle
	q.errorUserQueriesGauge.Set(0)
	q.errorUserQueries.Reset()

	// Reset cache hit/miss counters when we update (cache miss)
	q.cacheHits.Set(0)
	q.cacheMiss.Set(1)

	isPrimary, err := q.instance.IsPrimary()
	if err != nil {
		q.errorUserQueriesGauge.Set(1)
		q.errorUserQueries.WithLabelValues("isPrimary: " + err.Error()).Inc()
		return fmt.Errorf("while updating query metrics, could not check for primary: %w", err)
	}

	q.createMetricsFromUserQueries(isPrimary)
	q.timeLastUpdated = time.Now()
	// Update the timestamp metric to reflect when metrics were last computed
	q.lastUpdateTimestamp.Set(float64(q.timeLastUpdated.Unix()))
	return nil
}

// ShouldUpdate finds if the metrics from queries need to be rerun,
// or the cached values can be used
func (q *QueriesCollector) ShouldUpdate(ttl time.Duration) bool {
	q.metricsMutex.RLock()
	defer q.metricsMutex.RUnlock()
	return q.timeLastUpdated.IsZero() || time.Since(q.timeLastUpdated) > ttl
}

// RecordCacheHit increments the cache hit counter for the current cache period
func (q *QueriesCollector) RecordCacheHit() {
	q.metricsMutex.Lock()
	defer q.metricsMutex.Unlock()
	q.cacheHits.Add(1)
}

// Collect sends the pre-computed metrics to the output channel.
// These metrics were cached during the last Update() call and are not
// fetched from the database during collection.
func (q *QueriesCollector) Collect(ch chan<- prometheus.Metric) {
	// Guard the snapshot read and error metrics collection
	q.metricsMutex.RLock()
	defer q.metricsMutex.RUnlock()
	for _, m := range q.computedMetrics {
		ch <- m
	}
	// Add errors into errorUserQueriesVec and errorUserQueriesGauge metrics
	q.errorUserQueriesGauge.Collect(ch)
	q.errorUserQueries.Collect(ch)
	q.lastUpdateTimestamp.Collect(ch)
	q.cacheHits.Collect(ch)
	q.cacheMiss.Collect(ch)
}

func (q *QueriesCollector) createMetricsFromUserQueries(isPrimary bool) {
	// In case more than one user query specify a pattern in target_databases,
	// we need to get them just once
	var allAccessibleDatabasesCache []string

	var generatedMetrics []prometheus.Metric
	for name, userQuery := range q.userQueries {
		queryLogger := log.WithValues("query", name)
		queryRunner := QueryRunner{
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
			db, err := q.instance.ConnectionPool().Connection(targetDatabase)
			if err != nil {
				q.reportUserQueryErrorMetric(name + ": " + err.Error())
				continue
			}

			computedMetrics, err := queryRunner.computeMetrics(db)
			if err != nil {
				queryLogger.Error(err, "Error collecting user query",
					"targetDatabase", targetDatabase)
				// Increment metrics counters.
				q.reportUserQueryErrorMetric(name + " on db " + targetDatabase + ": " + err.Error())
			}
			generatedMetrics = append(generatedMetrics, computedMetrics...)
		}
	}
	q.computedMetrics = generatedMetrics
}

func (q *QueriesCollector) toBeChecked(name string, userQuery UserQuery, isPrimary bool, queryLogger log.Logger) bool {
	if (userQuery.Primary || userQuery.Master) && !isPrimary { // wokeignore:rule=master
		queryLogger.Debug("Skipping because runs only on primary")
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
			queryLogger.Debug("Skipping because runs only on other postgres versions",
				"runOnServer", runOnServer)
			return false
		}
	}

	return true
}

func (q *QueriesCollector) reportUserQueryErrorMetric(label string) {
	q.errorUserQueries.WithLabelValues(label).Inc()
	q.errorUserQueriesGauge.Set(1)
}

func (q *QueriesCollector) checkRunOnServerMatches(runOnServer string, name string) (bool, error) {
	// The instance will only get the PostgreSQL version one time
	// and then cache the result, so this is not a real query
	pgVersion, err := q.instance.GetPgVersion()
	if err != nil {
		log.Error(err, "while parsing runOnServer queries")
		return false, err
	}

	isVersionInRange, err := semver.ParseRange(runOnServer)
	if err != nil {
		log.Error(err, "while parsing runOnServer version range",
			"runOnServer", runOnServer, "query", name)
		return false, err
	}

	return isVersionInRange(pgVersion), nil
}

func (q *QueriesCollector) expandTargetDatabases(
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

func (q *QueriesCollector) getAllAccessibleDatabases() ([]string, error) {
	conn, err := q.instance.ConnectionPool().Connection(q.defaultDBName)
	if err != nil {
		return nil, fmt.Errorf("while connecting to expand target_database *: %w", err)
	}
	tx, err := createMonitoringTx(conn)
	if err != nil {
		return nil, fmt.Errorf("while creating monitoring tx to retrieve accessible databases list: %w", err)
	}
	defer func() {
		if err := tx.Commit(); err != nil {
			log.Error(err, "Error while committing monitoring tx to retrieve accessible databases list")
		}
	}()
	databases, errors := postgresutils.GetAllAccessibleDatabases(tx, "datallowconn AND NOT datistemplate")
	if errors != nil {
		return nil, fmt.Errorf("while discovering databases for metrics: %v", errors)
	}
	return databases, nil
}

// Describe implements the prometheus.Collector and defines the metrics with return
func (q *QueriesCollector) Describe(ch chan<- *prometheus.Desc) {
	for name, userQuery := range q.userQueries {
		collector := QueryRunner{
			namespace:     name,
			userQuery:     userQuery,
			columnMapping: q.mappings[name],
		}

		collector.describe(ch)
	}

	// add error user queries description
	q.errorUserQueries.Describe(ch)
	q.errorUserQueriesGauge.Describe(ch)
	q.lastUpdateTimestamp.Describe(ch)
	q.cacheHits.Describe(ch)
	q.cacheMiss.Describe(ch)
}

// NewQueriesCollector creates a new PgCollector working over a set of custom queries
// supplied by the user
func NewQueriesCollector(
	name string,
	instance *postgres.Instance,
	defaultDBName string,
) *QueriesCollector {
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
		lastUpdateTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: name,
			Name:      "last_update_timestamp",
			Help:      "Timestamp of the last metrics update.",
		}),
		cacheHits: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: name,
			Name:      "cache_hits",
			Help:      "Total number of hits for the current cache.",
		}),
		cacheMiss: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: name,
			Name:      "cache_miss",
			Help:      "Indicator: 1 if metrics were recomputed on last update (cache miss), 0 if cache used.",
		}),
	}
}

// ParseQueries parses a YAML file containing custom queries and add it
// to the set of gathered one
func (q *QueriesCollector) ParseQueries(customQueries []byte) error {
	var err error

	parsedQueries, err := ParseQueries(customQueries)
	if err != nil {
		return err
	}
	for name, query := range parsedQueries {
		if _, found := q.userQueries[name]; found {
			log.Warning("Query with the same name already found. Overwriting the existing one.",
				"queryName",
				name)
		}

		q.userQueries[name] = query
		// For the metric namespace, override the value included in the key with the query name, if it exists
		metricMapNamespace := name
		if query.Name != "" {
			metricMapNamespace = query.Name
		}
		q.mappings[name], q.variableLabels[name] = query.ToMetricMap(
			fmt.Sprintf("%v_%v", q.Name(), metricMapNamespace))
	}

	return nil
}

// InjectUserQueries injects the passed queries
func (q *QueriesCollector) InjectUserQueries(defaultQueries UserQueries) {
	if q == nil {
		return
	}

	for name, query := range defaultQueries {
		q.userQueries[name] = query
		q.mappings[name], q.variableLabels[name] = query.ToMetricMap(
			fmt.Sprintf("%v_%v", q.Name(), name))
	}
}

// QueryRunner computes a custom user query and generates Prometheus metrics
// from the results
type QueryRunner struct {
	namespace      string
	userQuery      UserQuery
	columnMapping  MetricMapSet
	variableLabels VariableSet
}

// computeMetrics runs the queries and generates prometheus metrics from them
func (c QueryRunner) computeMetrics(conn *sql.DB) ([]prometheus.Metric, error) {
	tx, err := createMonitoringTx(conn)
	if err != nil {
		return nil, err
	}
	var computedMetrics []prometheus.Metric

	defer func() {
		if err := tx.Commit(); err != nil {
			log.Error(err, "Error while committing metrics extraction")
		}
	}()

	shouldBeCollected, err := c.userQuery.isCollectable(tx)
	if err != nil {
		return nil, err
	}

	if !shouldBeCollected {
		return nil, nil
	}

	rows, err := tx.Query(c.userQuery.Query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warning("Error while closing metrics extraction",
				"err", err.Error())
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	columnData := make([]any, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range columnData {
		scanArgs[i] = &columnData[i]
	}

	if len(columns) != len(c.columnMapping) {
		log.Warning("Columns number mismatch",
			"name", c.namespace,
			"columnNumberFromDB", len(columns),
			"columnNumberFromConfiguration", len(c.columnMapping))
		return nil, nil
	}

	for rows.Next() {
		if err = rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		labels, done := c.listLabels(columns, columnData)
		if done {
			computedMetrics = append(computedMetrics, c.createMetricsFromColumns(columns, columnData, labels)...)
		}
	}
	if err := rows.Err(); err != nil {
		log.Warning("Error while loading metrics",
			"err", err.Error())
		return nil, err
	}

	return computedMetrics, nil
}

// Collect the list of labels from the database, and returns true if the
// label extraction succeeded, false otherwise
func (c QueryRunner) listLabels(columns []string, columnData []any) ([]string, bool) {
	var labels []string
	for idx, columnName := range columns {
		if mapping, ok := c.columnMapping[columnName]; ok && mapping.Label {
			value, ok := postgresutils.DBToString(columnData[idx])
			if !ok {
				log.Warning("Label value cannot be converted to string",
					"value", value,
					"mapping", mapping)
				return nil, false
			}

			labels = append(labels, value)
		}
	}
	return labels, true
}

// createMetricsFromColumns generates Prometheus metrics from the result columns
func (c QueryRunner) createMetricsFromColumns(
	columns []string,
	columnData []any,
	labels []string,
) []prometheus.Metric {
	var computedMetrics []prometheus.Metric
	for idx, columnName := range columns {
		mapping, ok := c.columnMapping[columnName]
		if !ok {
			log.Warning("Missing mapping for column", "column", columnName, "mapping", c.columnMapping)
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
				m := c.createHistogramMetric(mapping, histogramData, labels)
				if m != nil {
					computedMetrics = append(computedMetrics, m)
				}
			}

		default:
			m := c.createConstMetric(mapping, columnData[idx], labels)
			if m != nil {
				computedMetrics = append(computedMetrics, m)
			}
		}
	}
	return computedMetrics
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

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Set the application name
	_, err = tx.Exec("SET application_name TO cnpg_metrics_exporter")
	if err != nil {
		return nil, err
	}

	// Ensure standard_conforming_strings is enforced
	_, err = tx.Exec("SET standard_conforming_strings TO on")
	if err != nil {
		return nil, err
	}

	// Set the pg_monitor role
	_, err = tx.Exec("SET ROLE TO pg_monitor")

	return tx, err
}

// describe puts in the channel the metadata we have for the queries we collect
func (c QueryRunner) describe(ch chan<- *prometheus.Desc) {
	for _, mapSet := range c.columnMapping {
		ch <- mapSet.Desc
	}
}

// createConstMetric reports to the prometheus library a constant metric
func (c QueryRunner) createConstMetric(
	mapping MetricMap, value any, variableLabels []string,
) prometheus.Metric {
	if mapping.Conversion == nil {
		log.Warning("Missing conversion while parsing value",
			"namespace", c.namespace,
			"value", value,
			"mapping", mapping)
		return nil
	}

	floatData, ok := mapping.Conversion(value)
	if !ok {
		log.Warning("Error while parsing value",
			"namespace", c.namespace,
			"value", value,
			"mapping", mapping)
		return nil
	}

	// Generate the metric
	metric, err := prometheus.NewConstMetric(mapping.Desc, mapping.Vtype, floatData, variableLabels...)
	if err != nil {
		log.Error(err, "while collecting constant metric", "metric", mapping.Name)
		return nil
	}
	return metric
}

// createHistogramMetric reports to the prometheus library an histogram-based metric
func (c QueryRunner) createHistogramMetric(
	mapping MetricMap,
	columnData *histogram.Value,
	variableLabels []string,
) prometheus.Metric {
	metric, err := prometheus.NewConstHistogram(
		mapping.Desc,
		columnData.Count, columnData.Sum, columnData.Buckets,
		variableLabels...,
	)
	if err != nil {
		log.Error(err, "while collecting histogram metric", "metric", mapping.Name)
		return nil
	}
	return metric
}

// PluginCollector is the interface for collecting metrics from plugins
type PluginCollector interface {
	// Collect collects the metrics from the plugins
	Collect(ctx context.Context, ch chan<- prometheus.Metric, cluster *apiv1.Cluster) error
	// Describe describes the metrics from the plugins
	Describe(ctx context.Context, ch chan<- *prometheus.Desc, cluster *apiv1.Cluster)
}

type pluginCollector struct {
	pluginRepository repository.Interface
}

// NewPluginCollector creates a new PluginCollector that collects metrics from plugins
func NewPluginCollector(
	pluginRepository repository.Interface,
) PluginCollector {
	return &pluginCollector{pluginRepository: pluginRepository}
}

func (p *pluginCollector) Describe(ctx context.Context, ch chan<- *prometheus.Desc, cluster *apiv1.Cluster) {
	contextLogger := log.FromContext(ctx).WithName("plugin_metrics_describe")

	if len(p.getEnabledPluginNames(cluster)) == 0 {
		contextLogger.Trace("No plugins enabled for metrics collection")
		return
	}

	cli, err := p.getClient(ctx, cluster)
	if err != nil {
		contextLogger.Error(err, "failed to get plugin client")
		return
	}
	defer cli.Close(ctx)

	pluginsMetrics, err := cli.GetMetricsDefinitions(ctx, cluster)
	if err != nil {
		contextLogger.Error(err, "failed to get plugin metrics")
		return
	}

	for _, metric := range pluginsMetrics {
		ch <- metric.Desc
	}
}

func (p *pluginCollector) Collect(ctx context.Context, ch chan<- prometheus.Metric, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("plugin_metrics_collect")

	if len(p.getEnabledPluginNames(cluster)) == 0 {
		contextLogger.Trace("No plugins enabled for metrics collection")
		return nil
	}

	cli, err := p.getClient(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to get plugin client: %w", err)
	}
	defer cli.Close(ctx)

	definitions, err := cli.GetMetricsDefinitions(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to get plugin metrics during collect: %w", err)
	}

	res, err := cli.CollectMetrics(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to collect metrics from plugins: %w", err)
	}

	return sendPluginMetrics(definitions, res, ch)
}

func sendPluginMetrics(
	definitions pluginClient.PluginMetricDefinitions,
	metrics []*metrics.CollectMetric,
	ch chan<- prometheus.Metric,
) error {
	for _, metric := range metrics {
		definition := definitions.Get(metric.FqName)
		if definition == nil {
			return fmt.Errorf("metric definition not found for fqName: %s", metric.FqName)
		}

		m, err := prometheus.NewConstMetric(definition.Desc, definition.ValueType, metric.Value, metric.VariableLabels...)
		if err != nil {
			return fmt.Errorf("failed to create metric %s: %w", metric.FqName, err)
		}
		ch <- m
	}
	return nil
}

func (p *pluginCollector) getClient(ctx context.Context, cluster *apiv1.Cluster) (pluginClient.Client, error) {
	pluginLoadingContext, cancelPluginLoading := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPluginLoading()

	return pluginClient.WithPlugins(
		pluginLoadingContext,
		p.pluginRepository,
		p.getEnabledPluginNames(cluster)...,
	)
}

func (p *pluginCollector) getEnabledPluginNames(cluster *apiv1.Cluster) []string {
	enabledPluginNames := cluster.GetInstanceEnabledPluginNames()

	// for backward compatibility, we also add the WAL archive plugin that initially didn't require
	// INSTANCE_SIDECAR_INJECTION
	if pluginWAL := cluster.GetEnabledWALArchivePluginName(); pluginWAL != "" {
		if !slices.Contains(enabledPluginNames, pluginWAL) {
			enabledPluginNames = append(enabledPluginNames, pluginWAL)
		}
	}

	return enabledPluginNames
}
