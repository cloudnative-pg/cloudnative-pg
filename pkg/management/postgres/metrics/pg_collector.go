/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package metrics enables to expose a set of metrics and collectors on a given postgres instance
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// PrometheusNamespace is the namespace to be used for all custom metrics exposed by instances
// or the operator
const PrometheusNamespace = "cnp"

// Exporter exports a set of metrics and collectors on a given postgres instance
type Exporter struct {
	instance *postgres.Instance
	Metrics  *metrics
	queries  *QueriesCollector
}

// metrics here are related to the exporter itself, which is instrumented to
// expose them
type metrics struct {
	CollectionsTotal   prometheus.Counter
	PgCollectionErrors *prometheus.CounterVec
	Error              prometheus.Gauge
	PostgreSQLUp       prometheus.Gauge
	CollectionDuration *prometheus.GaugeVec
	SwitchoverRequired prometheus.Gauge
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
		PostgreSQLUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "up",
			Help:      "1 if PostgreSQL is up, 0 otherwise.",
		}),
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
	}
}

// Describe implements prometheus.Collector, defining the Metrics we return.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.Metrics.CollectionsTotal.Desc()
	ch <- e.Metrics.Error.Desc()
	e.Metrics.PgCollectionErrors.Describe(ch)
	ch <- e.Metrics.PostgreSQLUp.Desc()
	ch <- e.Metrics.SwitchoverRequired.Desc()
	e.Metrics.CollectionDuration.Describe(ch)

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
	ch <- e.Metrics.PostgreSQLUp
	ch <- e.Metrics.SwitchoverRequired
	e.Metrics.CollectionDuration.Collect(ch)
}

func (e *Exporter) collectPgMetrics(ch chan<- prometheus.Metric) {
	e.Metrics.CollectionsTotal.Inc()
	collectionStart := time.Now()
	db, err := e.instance.GetSuperUserDB()
	if err != nil {
		log.Log.Error(err, "Error opening connection to PostgreSQL")
		e.Metrics.Error.Set(1)
		return
	}

	// First, let's check the connection. No need to proceed if this fails.
	if err := db.Ping(); err != nil {
		log.Log.Error(err, "Error pinging PostgreSQL")
		e.Metrics.PostgreSQLUp.Set(0)
		e.Metrics.Error.Set(1)
		e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())
		return
	}

	e.Metrics.PostgreSQLUp.Set(1)
	e.Metrics.Error.Set(0)
	e.Metrics.CollectionDuration.WithLabelValues("Collect.up").Set(time.Since(collectionStart).Seconds())

	// Work on predefined metrics and custom queries
	if e.queries != nil {
		label := "Collect." + e.queries.Name()
		collectionStart := time.Now()
		if err := e.queries.Collect(ch); err != nil {
			log.Log.Error(err, "Error during collection", "collector", e.queries.Name())
			e.Metrics.PgCollectionErrors.WithLabelValues(label).Inc()
			e.Metrics.Error.Set(1)
		}
		e.Metrics.CollectionDuration.WithLabelValues(label).Set(time.Since(collectionStart).Seconds())
	}
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
func (e *Exporter) SetCustomQueries(queries *QueriesCollector) {
	e.queries = queries
}
