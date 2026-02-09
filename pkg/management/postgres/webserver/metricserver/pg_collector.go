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

package metricserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/prometheus/client_golang/prometheus"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	m "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/metrics"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/local"
	postgresconf "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// PrometheusNamespace is the namespace to be used for all custom metrics exposed by instances
// or the operator
const PrometheusNamespace = "cnpg"

var synchronousStandbyNamesRegex = regexp.MustCompile(`(?:ANY|FIRST) ([0-9]+) \(.*\)`)

// Exporter exports a set of metrics and collectors on a given postgres instance
type Exporter struct {
	instance *postgres.Instance
	Metrics  *metrics
	queries  *m.QueriesCollector
	// this is used for two reasons:
	// - to ensure we are able to unit test
	// - to make the struct adhere to the composition pattern instead of hardcoding dependencies inside the functions
	getCluster func() (*apiv1.Cluster, error)

	// pluginCollector is used to collect metrics from plugins
	pluginCollector m.PluginCollector
}

// metrics here are related to the exporter itself, which is instrumented to
// expose them
type metrics struct {
	CollectionsTotal             prometheus.Counter
	PgCollectionErrors           *prometheus.CounterVec
	Error                        prometheus.Gauge
	PostgreSQLUp                 *prometheus.GaugeVec
	CollectionDuration           *prometheus.GaugeVec
	SwitchoverRequired           prometheus.Gauge
	SyncReplicas                 *prometheus.GaugeVec
	ReplicaCluster               prometheus.Gauge
	PgWALArchiveStatus           *prometheus.GaugeVec
	PgWALDirectory               *prometheus.GaugeVec
	PgVersion                    *prometheus.GaugeVec
	FirstRecoverabilityPoint     prometheus.Gauge
	LastAvailableBackupTimestamp prometheus.Gauge
	LastFailedBackupTimestamp    prometheus.Gauge
	FencingOn                    prometheus.Gauge
	PgStatWalMetrics             PgStatWalMetrics
	NodesUsed                    prometheus.Gauge
	DiskMetrics                  *DiskMetrics
}

// PgStatWalMetrics is available from PG14+
type PgStatWalMetrics struct {
	WalRecords     *prometheus.GaugeVec
	WalFpi         *prometheus.GaugeVec
	WalBytes       *prometheus.GaugeVec
	WALBuffersFull *prometheus.GaugeVec
	WalWrite       *prometheus.GaugeVec
	WalSync        *prometheus.GaugeVec
	WalWriteTime   *prometheus.GaugeVec
	WalSyncTime    *prometheus.GaugeVec
}

// NewExporter creates an exporter
func NewExporter(instance *postgres.Instance, pluginCollector m.PluginCollector) *Exporter {
	clusterGetter := local.NewClient().Cache().GetCluster
	return &Exporter{
		instance:        instance,
		Metrics:         newMetrics(),
		getCluster:      clusterGetter,
		pluginCollector: pluginCollector,
	}
}

// newMetrics returns collector metrics
func newMetrics() *metrics {
	subsystem := "collector"
	return &metrics{
		CollectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "collections_total",
			Help:      "Total number of times PostgreSQL was accessed for metrics.",
		}),
		PgCollectionErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "collection_errors_total",
			Help:      "Total errors occurred accessing PostgreSQL for metrics.",
		}, []string{"collector"}),
		Error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "last_collection_error",
			Help:      "1 if the last collection ended with error, 0 otherwise.",
		}),
		PostgreSQLUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "up",
			Help:      "1 if PostgreSQL is up, 0 otherwise.",
		}, []string{"cluster"}),
		CollectionDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "collection_duration_seconds",
			Help:      "Collection time duration in seconds",
		}, []string{"collector"}),
		SwitchoverRequired: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "manual_switchover_required",
			Help:      "1 if a manual switchover is required, 0 otherwise",
		}),
		SyncReplicas: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sync_replicas",
			Help:      "Number of requested synchronous replicas (synchronous_standby_names)",
		}, []string{"value"}),
		ReplicaCluster: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "replica_mode",
			Help:      "1 if the cluster is in replica mode, 0 otherwise",
		}),
		PgWALArchiveStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "pg_wal_archive_status",
			Help: fmt.Sprintf("Number of WAL segments in the '%s' directory (ready, done)",
				specs.PgWalArchiveStatusPath),
		}, []string{"value"}),
		PgVersion: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "postgres_version",
			Help:      "Postgres version",
		}, []string{"full", "cluster"}),
		PgWALDirectory: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "pg_wal",
			Help: fmt.Sprintf("Total size in bytes of WAL segments in the '%s' directory "+
				" computed as (wal_segment_size * count)",
				specs.PgWalPath),
		}, []string{"value"}),
		FirstRecoverabilityPoint: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "first_recoverability_point",
			Help: "The first point of recoverability for the cluster as a unix timestamp" +
				" (Deprecated)",
		}),
		LastAvailableBackupTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "last_available_backup_timestamp",
			Help:      "The last available backup as a unix timestamp (Deprecated)",
		}),
		LastFailedBackupTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "last_failed_backup_timestamp",
			Help:      "The last failed backup as a unix timestamp (Deprecated)",
		}),
		FencingOn: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "fencing_on",
			Help:      "1 if the instance is fenced, 0 otherwise",
		}),
		NodesUsed: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "nodes_used",
			Help: "NodesUsed represents the count of distinct nodes accommodating the instances. " +
				"A value of '-1' suggests that the metric is not available. " +
				"A value of '1' suggests that all instances are hosted on a single node, " +
				"implying the absence of High Availability (HA). Ideally this value " +
				"should match the number of instances in the cluster.",
		}),
		PgStatWalMetrics: PgStatWalMetrics{
			WalRecords: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_records",
				Help:      "Total number of WAL records generated. Only available on PG 14+",
			}, []string{"stats_reset"}),
			WalFpi: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_fpi",
				Help:      "Total number of WAL full page images generated. Only available on PG 14+",
			}, []string{"stats_reset"}),
			WalBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_bytes",
				Help:      "Total amount of WAL generated in bytes. Only available on PG 14+",
			}, []string{"stats_reset"}),
			WALBuffersFull: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_buffers_full",
				Help:      "Number of times WAL data was written to disk because WAL buffers became full. Only available on PG 14+",
			}, []string{"stats_reset"}),
			WalWrite: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_write",
				Help: "Number of times WAL buffers were written out to disk via XLogWrite request." +
					" Only available on PG 14 to 17.",
			}, []string{"stats_reset"}),
			WalSync: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_sync",
				Help: "Number of times WAL files were synced to disk via issue_xlog_fsync request " +
					"(if fsync is on and wal_sync_method is either fdatasync, fsync or fsync_writethrough, otherwise zero)." +
					" Only available on PG 14 to 17.",
			}, []string{"stats_reset"}),
			WalWriteTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_write_time",
				Help: "Total amount of time spent writing WAL buffers to disk via XLogWrite request, in milliseconds " +
					"(if track_wal_io_timing is enabled, otherwise zero). This includes the sync time when wal_sync_method " +
					"is either open_datasync or open_sync." +
					" Only available on PG 14 to 17.",
			}, []string{"stats_reset"}),
			WalSyncTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: PrometheusNamespace,
				Subsystem: subsystem,
				Name:      "wal_sync_time",
				Help: "Total amount of time spent syncing WAL files to disk via issue_xlog_fsync request, in milliseconds " +
					"(if track_wal_io_timing is enabled, fsync is on, and wal_sync_method is either fdatasync, fsync or " +
					"fsync_writethrough, otherwise zero). Only available on PG 14 to 17.",
			}, []string{"stats_reset"}),
		},
		DiskMetrics: newDiskMetrics(),
	}
}

// Describe implements prometheus.Collector, defining the Metrics we return.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.Metrics.CollectionsTotal.Desc()
	ch <- e.Metrics.Error.Desc()
	e.Metrics.PgCollectionErrors.Describe(ch)
	e.Metrics.PostgreSQLUp.Describe(ch)
	ch <- e.Metrics.SwitchoverRequired.Desc()
	e.Metrics.CollectionDuration.Describe(ch)
	e.Metrics.SyncReplicas.Describe(ch)
	ch <- e.Metrics.ReplicaCluster.Desc()
	e.Metrics.PgWALArchiveStatus.Describe(ch)
	e.Metrics.PgWALDirectory.Describe(ch)
	e.Metrics.PgVersion.Describe(ch)
	e.Metrics.FirstRecoverabilityPoint.Describe(ch)
	e.Metrics.FencingOn.Describe(ch)
	e.Metrics.LastFailedBackupTimestamp.Describe(ch)
	e.Metrics.LastAvailableBackupTimestamp.Describe(ch)
	e.Metrics.NodesUsed.Describe(ch)
	e.Metrics.DiskMetrics.describe(ch)

	if e.queries != nil {
		e.queries.Describe(ch)
	}

	if version, _ := e.instance.GetPgVersion(); version.Major >= 14 {
		e.Metrics.PgStatWalMetrics.WalRecords.Describe(ch)
		e.Metrics.PgStatWalMetrics.WalFpi.Describe(ch)
		e.Metrics.PgStatWalMetrics.WalBytes.Describe(ch)
		e.Metrics.PgStatWalMetrics.WALBuffersFull.Describe(ch)
		if version.Major < 18 {
			e.Metrics.PgStatWalMetrics.WalWrite.Describe(ch)
			e.Metrics.PgStatWalMetrics.WalSync.Describe(ch)
			e.Metrics.PgStatWalMetrics.WalWriteTime.Describe(ch)
			e.Metrics.PgStatWalMetrics.WalSyncTime.Describe(ch)
		}
	}

	if cluster, _ := e.getCluster(); cluster != nil {
		e.pluginCollector.Describe(context.Background(), ch, cluster)
	}
}

// Collect implements prometheus.Collector, collecting the Metrics values to
// export.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	log.Debug("collecting Postgres instance metrics")
	ctx := context.Background()
	e.updateInstanceMetrics(ctx)
	e.collectInstanceMetrics(ch)

	if e.queries != nil {
		var defaultClusterForTTL apiv1.Cluster
		ttl := defaultClusterForTTL.GetMetricsQueriesTTL().Duration
		if cluster, _ := e.getCluster(); cluster != nil {
			ttl = cluster.GetMetricsQueriesTTL().Duration
		}

		if e.queries.ShouldUpdate(ttl) {
			log.Trace("no cache, updating metrics from queries", "ttlSeconds", ttl.Seconds())
			e.updateMetricsFromQueries()
		} else {
			log.Debug("using cached metrics from queries, as TTL is not exceeded",
				"ttlSeconds", ttl.Seconds())
			e.queries.RecordCacheHit()
		}
		log.Debug("collecting metrics from queries")
		e.queries.Collect(ch)
	}
}

func (e *Exporter) updateMetricsFromQueries() {
	// Work on predefined metrics and custom queries
	label := "Collect." + e.queries.Name()
	collectionStart := time.Now()
	log.Debug("updating metrics from queries")
	if err := e.queries.Update(); err != nil {
		log.Error(err, "Error during query update", "collector", e.queries.Name())
		e.Metrics.PgCollectionErrors.WithLabelValues(label).Inc()
		e.Metrics.Error.Set(1)
	}
	log.Debug("created metrics from user queries",
		"duration", time.Since(collectionStart).Seconds())
	e.Metrics.CollectionDuration.WithLabelValues(label).Set(time.Since(collectionStart).Seconds())
}

// collectInstanceMetrics sends the computed instance metrics to the channel
func (e *Exporter) collectInstanceMetrics(ch chan<- prometheus.Metric) {
	ch <- e.Metrics.CollectionsTotal
	ch <- e.Metrics.Error
	e.Metrics.PgCollectionErrors.Collect(ch)
	e.Metrics.PostgreSQLUp.Collect(ch)
	ch <- e.Metrics.SwitchoverRequired
	e.Metrics.CollectionDuration.Collect(ch)
	e.Metrics.SyncReplicas.Collect(ch)
	ch <- e.Metrics.ReplicaCluster
	e.Metrics.PgWALArchiveStatus.Collect(ch)
	e.Metrics.PgWALDirectory.Collect(ch)
	e.Metrics.PgVersion.Collect(ch)
	e.Metrics.FirstRecoverabilityPoint.Collect(ch)
	e.Metrics.FencingOn.Collect(ch)
	e.Metrics.LastFailedBackupTimestamp.Collect(ch)
	e.Metrics.LastAvailableBackupTimestamp.Collect(ch)
	e.Metrics.NodesUsed.Collect(ch)
	e.Metrics.DiskMetrics.collect(ch)

	if version, _ := e.instance.GetPgVersion(); version.Major >= 14 {
		e.Metrics.PgStatWalMetrics.WalRecords.Collect(ch)
		e.Metrics.PgStatWalMetrics.WalFpi.Collect(ch)
		e.Metrics.PgStatWalMetrics.WalBytes.Collect(ch)
		e.Metrics.PgStatWalMetrics.WALBuffersFull.Collect(ch)
		if version.Major < 18 {
			e.Metrics.PgStatWalMetrics.WalWrite.Collect(ch)
			e.Metrics.PgStatWalMetrics.WalSync.Collect(ch)
			e.Metrics.PgStatWalMetrics.WalWriteTime.Collect(ch)
			e.Metrics.PgStatWalMetrics.WalSyncTime.Collect(ch)
		}
	}

	if cluster, _ := e.getCluster(); cluster != nil {
		if err := e.pluginCollector.Collect(context.Background(), ch, cluster); err != nil {
			log.Error(err, "error while collecting plugin metrics")
			e.Metrics.Error.Set(1)
			e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PluginMetrics").Inc()
		}
	}
}

// updateInstanceMetrics updates metrics from the instance manager
func (e *Exporter) updateInstanceMetrics(ctx context.Context) {
	e.Metrics.CollectionsTotal.Inc()
	collectionStart := time.Now()
	if e.instance.IsFenced() {
		e.Metrics.FencingOn.Set(1)
		log.Info("metrics collection skipped due to fencing")
		return
	}
	e.Metrics.FencingOn.Set(0)

	if e.instance.MightBeUnavailable() {
		log.Info("metrics collection skipped due to instance still being down")
		e.Metrics.Error.Set(0)
		return
	}

	db, err := e.instance.GetSuperUserDB()
	if err != nil {
		log.Error(err, "Error opening connection to PostgreSQL")
		e.Metrics.Error.Set(1)
		return
	}

	// First, let's check the connection. No need to proceed if this fails.
	if err := db.Ping(); err != nil {
		log.Warning("Unable to collect metrics", "error", err)
		e.Metrics.PostgreSQLUp.WithLabelValues(e.instance.GetClusterName()).Set(0)
		e.Metrics.Error.Set(1)
		e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())
		return
	}

	e.Metrics.PostgreSQLUp.WithLabelValues(e.instance.GetClusterName()).Set(1)
	e.Metrics.Error.Set(0)
	e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())

	isPrimary, err := e.instance.IsPrimary()
	if err != nil {
		log.Error(err, "unable to get if primary")
	}

	e.collectNodesUsed()

	// metrics collected only on primary server
	if isPrimary {
		// getting required synchronous standby number from postgres itself
		e.collectFromPrimarySynchronousStandbysNumber(db)

		// getting the first point of recoverability
		e.collectFromPrimaryFirstPointOnTimeRecovery()

		// getting the last available backup timestamp
		e.collectFromPrimaryLastAvailableBackupTimestamp()

		e.collectFromPrimaryLastFailedBackupTimestamp()
	}

	// Collect disk usage metrics (statfs-based)
	collectDiskUsageMetrics(e)

	// Collect WAL health metrics
	collectWALHealthMetrics(ctx, e, db, isPrimary)

	if err := collectPGWalArchiveMetric(e); err != nil {
		log.Error(err, "while collecting WAL archive metrics", "path", specs.PgWalArchiveStatusPath)
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PgWALArchiveStats").Inc()
		e.Metrics.PgWALArchiveStatus.Reset()
	}

	if err := collectPGWalSettings(e, db); err != nil {
		log.Error(err, "while collecting WAL settings", "path", specs.PgWalPath)
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PGWalSettings").Inc()
		e.Metrics.PgWALDirectory.Reset()
	}

	if err := collectPGVersion(e); err != nil {
		log.Error(err, "while collecting PGVersion metrics")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PGVersion").Inc()
		e.Metrics.PgVersion.Reset()
	}

	if version, _ := e.instance.GetPgVersion(); version.Major >= 14 {
		if err := collectPGStatWAL(e); err != nil {
			log.Error(err, "while collecting pg_stat_wal")
			e.Metrics.Error.Set(1)
			e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PGWALStat").Inc()
		}
	}
}

func (e *Exporter) setTimestampMetric(
	gauge prometheus.Gauge,
	errorLabel string,
	getTimestampFunc func(cluster *apiv1.Cluster) string,
) {
	cluster, err := e.getCluster()
	// there isn't a cached object yet
	if errors.Is(err, cache.ErrCacheMiss) {
		return
	}
	// programmatic error, we should report that
	if err != nil {
		log.Error(err, "error while retrieving cluster cache object")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(errorLabel).Inc()
		// if there is a programmatic error in the cache we should reset any potential data because it cannot be
		// trusted as still valid
		gauge.Set(0)
		return
	}

	ts := getTimestampFunc(cluster)
	if ts == "" {
		// if there is no timestamp we report the timestamp metric with the zero value
		gauge.Set(0)
		return
	}

	parsedTS, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		log.Error(err, "while collecting timestamp", "errorLabel", errorLabel)
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(errorLabel).Inc()
		gauge.Set(0)
		return
	}

	// The Prometheus best practice documentation refers to
	// exposing timestamps using the relative Unix timestamp
	// number. See:
	// https://prometheus.io/docs/practices/instrumentation/#timestamps-not-time-since
	gauge.Set(float64(parsedTS.Unix()))
}

func (e *Exporter) collectNodesUsed() {
	const notExtractedValue float64 = -1

	cluster, err := e.getCluster()
	if err != nil {
		log.Error(err, "unable to collect metrics")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.NodesUsed").Inc()
		e.Metrics.NodesUsed.Set(notExtractedValue)
		return
	}

	if !cluster.Status.Topology.SuccessfullyExtracted {
		e.Metrics.NodesUsed.Set(notExtractedValue)
		return
	}

	e.Metrics.NodesUsed.Set(float64(cluster.Status.Topology.NodesUsed))
}

func (e *Exporter) collectFromPrimaryLastFailedBackupTimestamp() {
	const errorLabel = "Collect.LastFailedBackupTimestamp"
	e.setTimestampMetric(e.Metrics.LastFailedBackupTimestamp, errorLabel, func(cluster *apiv1.Cluster) string {
		return cluster.Status.LastFailedBackup //nolint:staticcheck
	})
}

func (e *Exporter) collectFromPrimaryLastAvailableBackupTimestamp() {
	const errorLabel = "Collect.LastAvailableBackupTimestamp"
	e.setTimestampMetric(e.Metrics.LastAvailableBackupTimestamp, errorLabel, func(cluster *apiv1.Cluster) string {
		return cluster.Status.LastSuccessfulBackup //nolint:staticcheck
	})
}

func (e *Exporter) collectFromPrimaryFirstPointOnTimeRecovery() {
	const errorLabel = "Collect.FirstRecoverabilityPoint"
	e.setTimestampMetric(e.Metrics.FirstRecoverabilityPoint, errorLabel, func(cluster *apiv1.Cluster) string {
		return cluster.Status.FirstRecoverabilityPoint //nolint:staticcheck
	})
}

func (e *Exporter) collectFromPrimarySynchronousStandbysNumber(db *sql.DB) {
	nStandbys, err := getRequestedSynchronousStandbysNumber(db)
	if err != nil {
		log.Error(err, "unable to collect metrics")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.SynchronousStandbys").Inc()
		e.Metrics.SyncReplicas.WithLabelValues("observed").Set(-1)
		return
	}

	e.Metrics.SyncReplicas.WithLabelValues("observed").Set(float64(nStandbys))
}

func collectPGVersion(e *Exporter) error {
	semanticVersion, err := e.instance.GetPgVersion()
	if err != nil {
		return err
	}

	// we use patch instead of minor because postgres doesn't use semantic versioning
	majorMinor := fmt.Sprintf("%d.%d", semanticVersion.Major, semanticVersion.Patch)
	version, err := strconv.ParseFloat(majorMinor, 64)
	if err != nil {
		return err
	}
	e.Metrics.PgVersion.WithLabelValues(majorMinor, e.instance.GetClusterName()).Set(version)

	return nil
}

// getRequestedSynchronousStandbysNumber returns the number of requested synchronous standbys
// Example: FIRST 2 (node1,node2) will return 2, ANY 4 (node1) will return 4.
// If the query fails, it will return 0 and an error.
func getRequestedSynchronousStandbysNumber(db *sql.DB) (int, error) {
	var syncReplicasFromConfig string
	err := db.QueryRow(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).
		Scan(&syncReplicasFromConfig)
	if err != nil || syncReplicasFromConfig == "" {
		return 0, err
	}
	if !synchronousStandbyNamesRegex.MatchString(syncReplicasFromConfig) {
		return 0, fmt.Errorf("not matching synchronous standby names regex: %s", syncReplicasFromConfig)
	}
	return strconv.Atoi(synchronousStandbyNamesRegex.FindStringSubmatch(syncReplicasFromConfig)[1])
}

// PgCollector is the interface for all the collectors that need to do queries
// on PostgreSQL to gather the results
type PgCollector interface {
	// Name is the unique name of the collector
	Name() string

	// Collect collects data and send the metrics on the channel
	Collect(ch chan<- prometheus.Metric) error

	// Describe collects metadata about the metrics we work with
	Describe(ch chan<- *prometheus.Desc)
}

// SetCustomQueries sets the custom queries from the passed content
func (e *Exporter) SetCustomQueries(queries *m.QueriesCollector) {
	e.queries = queries
}

// DefaultQueries is the set of default queries for postgresql
var DefaultQueries = m.UserQueries{
	"collector": m.UserQuery{
		Query: "SELECT pg_catalog.current_database() as datname, relpages as lo_pages " +
			"FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON (n.oid = c.relnamespace) " +
			"WHERE n.nspname = 'pg_catalog' AND c.relname = 'pg_largeobject';",
		TargetDatabases: []string{"*"},
		Metrics: []m.Mapping{
			{
				"datname": m.ColumnMapping{
					Usage:       m.LABEL,
					Description: "Name of the database",
				},
			},
			{
				"lo_pages": m.ColumnMapping{
					Usage:       m.GAUGE,
					Description: "Estimated number of pages in the pg_largeobject table",
				},
			},
		},
	},
}
