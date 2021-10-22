/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package metricsserver enables to expose a set of metrics and collectors on a given postgres instance
package metricsserver

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/pgbouncer/config"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/pool"
)

// PrometheusNamespace is the namespace to be used for all custom metrics exposed by instances
// or the operator
const PrometheusNamespace = "cnp"

// Exporter exports a set of metrics and collectors on a given postgres instance
type Exporter struct {
	Metrics *metrics
	pool    *pool.ConnectionPool
}

// metrics here are related to the exporter itself, which is instrumented to
// expose them
type metrics struct {
	CollectionsTotal   prometheus.Counter
	PgCollectionErrors *prometheus.CounterVec
	Error              prometheus.Gauge
	CollectionDuration *prometheus.GaugeVec
	PgbouncerUp        prometheus.Gauge
	ShowLists          ShowListsMetrics
	ShowPools          *ShowPoolsMetrics
	ShowStats          *ShowStatsMetrics
}

// NewExporter creates an exporter
func NewExporter() *Exporter {
	return &Exporter{
		Metrics: newMetrics(),
	}
}

// newMetrics returns collector metrics
func newMetrics() *metrics {
	subsystem := "pgbouncer"
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
		PgbouncerUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "up",
			Help:      "1 if pgbouncer is up, 0 otherwise.",
		}),
		Error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "last_collection_error",
			Help:      "1 if the last collection ended with error, 0 otherwise.",
		}),
		CollectionDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "collection_duration_seconds",
			Help:      "Collection time duration in seconds",
		}, []string{"collector"}),
		ShowLists: NewShowListsMetrics(subsystem),
		ShowPools: NewShowPoolsMetrics(subsystem),
		ShowStats: NewShowStatsMetrics(subsystem),
	}
}

// Describe implements prometheus.Collector, defining the Metrics we return.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.Metrics.CollectionsTotal.Desc()
	ch <- e.Metrics.Error.Desc()
	e.Metrics.PgCollectionErrors.Describe(ch)
	e.Metrics.CollectionDuration.Describe(ch)
	e.Metrics.ShowLists.Describe(ch)
	e.Metrics.ShowPools.Describe(ch)
	e.Metrics.ShowStats.Describe(ch)
}

// Collect implements prometheus.Collector, collecting the Metrics values to
// export.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.collectPgBouncerMetrics(ch)

	ch <- e.Metrics.CollectionsTotal
	ch <- e.Metrics.Error
	e.Metrics.PgCollectionErrors.Collect(ch)
	e.Metrics.CollectionDuration.Collect(ch)
}

func (e *Exporter) collectPgBouncerMetrics(ch chan<- prometheus.Metric) {
	e.Metrics.CollectionsTotal.Inc()
	collectionStart := time.Now()
	defer func() {
		e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())
	}()
	db, err := e.GetPgBouncerDB()
	if err != nil {
		log.Error(err, "Error opening connection to PostgreSQL")
		e.Metrics.Error.Set(1)
		return
	}

	e.collectShowLists(ch, db)
	e.collectShowPools(ch, db)
	e.collectShowStats(ch, db)
}

// GetPgBouncerDB gets a connection to the admin user db "pgbouncer" on this instance
func (e *Exporter) GetPgBouncerDB() (*sql.DB, error) {
	return e.ConnectionPool().Connection("pgbouncer")
}

// ConnectionPool gets or initializes the connection pool for this instance
func (e *Exporter) ConnectionPool() *pool.ConnectionPool {
	if e.pool == nil {
		dsn := fmt.Sprintf(
			"host=%s port=%v user=%s sslmode=disable",
			config.PgBouncerSocketDir,
			config.PgBouncerPort,
			config.PgBouncerAdminUser,
		)

		e.pool = pool.NewConnectionPool(dsn)
	}

	return e.pool
}
