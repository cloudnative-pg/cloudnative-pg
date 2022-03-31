/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package metricserver

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	m "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/metrics"
	postgresconf "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// PrometheusNamespace is the namespace to be used for all custom metrics exposed by instances
// or the operator
const PrometheusNamespace = "cnp"

var synchronousStandbyNamesRegex = regexp.MustCompile(`ANY ([0-9]+) \(.*\)`)

// The wal_segment_size value in bytes
var walSegmentSize *int

// Exporter exports a set of metrics and collectors on a given postgres instance
type Exporter struct {
	instance *postgres.Instance
	Metrics  *metrics
	queries  *m.QueriesCollector
}

// metrics here are related to the exporter itself, which is instrumented to
// expose them
type metrics struct {
	CollectionsTotal         prometheus.Counter
	PgCollectionErrors       *prometheus.CounterVec
	Error                    prometheus.Gauge
	PostgreSQLUp             *prometheus.GaugeVec
	CollectionDuration       *prometheus.GaugeVec
	SwitchoverRequired       prometheus.Gauge
	SyncReplicas             *prometheus.GaugeVec
	ReplicaCluster           prometheus.Gauge
	PgWALArchiveStatus       *prometheus.GaugeVec
	PgWALDirectory           *prometheus.GaugeVec
	PgVersion                *prometheus.GaugeVec
	FirstRecoverabilityPoint prometheus.Gauge
	FencingOn                prometheus.Gauge
}

// NewExporter creates an exporter
func NewExporter(instance *postgres.Instance) *Exporter {
	return &Exporter{
		instance: instance,
		Metrics:  newMetrics(),
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
			Help:      "The first point of recoverability for the cluster as a unix timestamp",
		}),
		FencingOn: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "fencing_on",
			Help:      "1 if the instance is fenced, 0 otherwise",
		}),
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

	if e.queries != nil {
		e.queries.Describe(ch)
	}
}

// Collect implements prometheus.Collector, collecting the Metrics values to
// export.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.collectPgMetrics(ch)

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
}

func (e *Exporter) collectPgMetrics(ch chan<- prometheus.Metric) {
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
		e.Metrics.PostgreSQLUp.WithLabelValues(e.instance.ClusterName).Set(0)
		e.Metrics.Error.Set(1)
		e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())
		return
	}

	e.Metrics.PostgreSQLUp.WithLabelValues(e.instance.ClusterName).Set(1)
	e.Metrics.Error.Set(0)
	e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())

	// Work on predefined metrics and custom queries
	if e.queries != nil {
		label := "Collect." + e.queries.Name()
		collectionStart := time.Now()
		if err := e.queries.Collect(ch); err != nil {
			log.Error(err, "Error during collection", "collector", e.queries.Name())
			e.Metrics.PgCollectionErrors.WithLabelValues(label).Inc()
			e.Metrics.Error.Set(1)
		}
		e.Metrics.CollectionDuration.WithLabelValues(label).Set(time.Since(collectionStart).Seconds())
	}

	isPrimary, err := e.instance.IsPrimary()
	if err != nil {
		log.Error(err, "unable to get if primary")
	}

	// metrics collected only on primary server
	if isPrimary {
		// getting required synchronous standby number from postgres itself
		e.collectFromPrimarySynchronousStandbysNumber(db)

		// getting the first point of recoverability
		e.collectFromPrimaryFirstPointOnTimeRecovery()
	}

	if err := collectPGWalArchiveMetric(e); err != nil {
		log.Error(err, "while collecting WAL archive metrics", "path", specs.PgWalArchiveStatusPath)
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PgWALArchiveStats").Inc()
		e.Metrics.PgWALArchiveStatus.Reset()
	}

	if err := collectPGWalMetric(e, db); err != nil {
		log.Error(err, "while collecting WAL metrics", "path", specs.PgWalPath)
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PgWALStats").Inc()
		e.Metrics.PgWALDirectory.Reset()
	}

	if err := collectPGVersion(e); err != nil {
		log.Error(err, "while collecting PGVersion metrics")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues("Collect.PGVersion").Inc()
		e.Metrics.PgVersion.Reset()
	}
}

func (e *Exporter) collectFromPrimaryFirstPointOnTimeRecovery() {
	const errorLabel = "Collect.FirstRecoverabilityPoint"

	cluster, err := cache.LoadCluster()
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
		e.Metrics.FirstRecoverabilityPoint.Set(0)
		return
	}

	ts := cluster.Status.FirstRecoverabilityPoint
	// means no First recoverability point yet
	if ts == "" {
		return
	}

	parsedTS, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		log.Error(err, "while collecting parsing first recoverability point timestamp")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(errorLabel).Inc()
		// if we cannot parse FirstRecoverabilityPoint we should reset the potential existing value because it cannot be
		// trusted as still valid
		e.Metrics.FirstRecoverabilityPoint.Set(0)
		return
	}

	// The Prometheus best practice documentation refers to
	// exposing timestamps using the relative Unix timestamp
	// number. See:
	// https://prometheus.io/docs/practices/instrumentation/#timestamps-not-time-since
	e.Metrics.FirstRecoverabilityPoint.Set(float64(parsedTS.Unix()))
}

func (e *Exporter) collectFromPrimarySynchronousStandbysNumber(db *sql.DB) {
	nStandbys, err := getSynchronousStandbysNumber(db)
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

	majorMinor := fmt.Sprintf("%d.%d", semanticVersion.Major, semanticVersion.Minor)
	version, err := strconv.ParseFloat(majorMinor, 64)
	if err != nil {
		return err
	}

	majorMinorPatch := fmt.Sprintf("%s.%d", majorMinor, semanticVersion.Patch)
	e.Metrics.PgVersion.WithLabelValues(majorMinorPatch, e.instance.ClusterName).Set(version)

	return nil
}

func collectPGWalArchiveMetric(exporter *Exporter) error {
	ready, done, err := postgres.GetWALArchiveCounters()
	if err != nil {
		return err
	}

	exporter.Metrics.PgWALArchiveStatus.WithLabelValues("ready").Set(float64(ready))
	exporter.Metrics.PgWALArchiveStatus.WithLabelValues("done").Set(float64(done))
	return nil
}

var regexPGWalFileName = regexp.MustCompile("^[0-9A-F]{24}")

func collectPGWalMetric(exporter *Exporter, db *sql.DB) error {
	pgWalDir, err := os.Open(specs.PgWalPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = pgWalDir.Close()
	}()
	files, err := pgWalDir.Readdirnames(-1)
	if err != nil {
		return err
	}
	var count int
	for _, file := range files {
		if !regexPGWalFileName.MatchString(file) {
			continue
		}
		count++
	}

	exporter.Metrics.PgWALDirectory.WithLabelValues("count").Set(float64(count))
	WALSegmentSize, err := getWALSegmentSize(db)
	if err != nil {
		return err
	}
	exporter.Metrics.PgWALDirectory.WithLabelValues("size").Set(float64(count * WALSegmentSize))
	return nil
}

// We cache the value of wal_segment_size the first time we retrieve it from the database
func getWALSegmentSize(db *sql.DB) (int, error) {
	if walSegmentSize != nil {
		return *walSegmentSize, nil
	}
	var size int
	err := db.QueryRow("SELECT setting FROM pg_settings WHERE name='wal_segment_size'").
		Scan(&size)
	if err != nil {
		log.Error(err, "while getting the wal_segment_size value from the database")
		return 0, err
	}
	walSegmentSize = &size
	return *walSegmentSize, nil
}

func getSynchronousStandbysNumber(db *sql.DB) (int, error) {
	var syncReplicasFromConfig string
	err := db.QueryRow(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).
		Scan(&syncReplicasFromConfig)
	if err != nil || syncReplicasFromConfig == "" {
		return 0, err
	}
	if !synchronousStandbyNamesRegex.Match([]byte(syncReplicasFromConfig)) {
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
		Query: "SELECT current_database() as datname, relpages as lo_pages " +
			"FROM pg_class c JOIN pg_namespace n ON (n.oid = c.relnamespace) " +
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
