/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package metrics enables to expose a set of metrics and collectors on a given postgres instance
package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/log"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/postgres"
)

const namespace = "cnp"

// Exporter exports a set of metrics and collectors on a given postgres instance
type Exporter struct {
	instance     *postgres.Instance
	metrics      metrics
	pgCollectors []PgCollector
}

// metrics here are related to the exporter itself, which is instrumented to
// expose them
type metrics struct {
	CollectionsTotal   prometheus.Counter
	PgCollectionErrors *prometheus.CounterVec
	Error              prometheus.Gauge
	PostgreSQLUp       prometheus.Gauge
	CollectionDuration *prometheus.GaugeVec
}

// NewExporter creates an exporter
func NewExporter(instance *postgres.Instance) *Exporter {
	return &Exporter{
		instance:     instance,
		pgCollectors: newPgCollectors(instance),
		metrics:      newMetrics(),
	}
}

func newMetrics() metrics {
	subsystem := "collector"
	return metrics{
		CollectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "collections_total",
			Help:      "Total number of times PostgreSQL was accessed for metrics.",
		}),
		PgCollectionErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "collection_errors_total",
			Help:      "Total errors occurred accessing PostgreSQL for metrics.",
		}, []string{"collector"}),
		Error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "last_collection_error",
			Help:      "1 if the last collection ended with error, 0 otherwise.",
		}),
		PostgreSQLUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "up",
			Help:      "1 if PostgreSQL is up, 0 otherwise.",
		}),
		CollectionDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "collection_duration_seconds",
			Help:      "Collection time duration in seconds",
		}, []string{"collector"}),
	}
}

// newPgCollectors returns an array of the PgCollectors that will be run on the
// instance
func newPgCollectors(instance *postgres.Instance) []PgCollector {
	pgCollectors := []PgCollector{
		newPgStatArchiverCollector(instance),
	}
	return pgCollectors
}

// Describe implements prometheus.Collector, defining the metrics we return.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.metrics.CollectionsTotal.Desc()
	ch <- e.metrics.Error.Desc()
	e.metrics.PgCollectionErrors.Describe(ch)
	ch <- e.metrics.PostgreSQLUp.Desc()
	e.metrics.CollectionDuration.Describe(ch)
}

// Collect implements prometheus.Collector, collecting the metrics values to
// export.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.collectPgMetrics(ch)

	ch <- e.metrics.CollectionsTotal
	ch <- e.metrics.Error
	e.metrics.PgCollectionErrors.Collect(ch)
	ch <- e.metrics.PostgreSQLUp
	e.metrics.CollectionDuration.Collect(ch)
}

func (e *Exporter) collectPgMetrics(ch chan<- prometheus.Metric) {
	e.metrics.CollectionsTotal.Inc()
	collectionStart := time.Now()
	// TODO: should be GetApplicationDB, but that's not local
	db, err := e.instance.GetSuperUserDB()
	if err != nil {
		log.Log.Error(err, "Error opening connection to PostgreSQL")
		e.metrics.Error.Set(1)
		return
	}

	// First, let's check the connection. No need to proceed if this fails.
	if err := db.Ping(); err != nil {
		log.Log.Error(err, "Error pinging PostgreSQL")
		e.metrics.PostgreSQLUp.Set(0)
		e.metrics.Error.Set(1)
		e.metrics.CollectionDuration.WithLabelValues("collect.up").Set(time.Since(collectionStart).Seconds())
		return
	}

	e.metrics.PostgreSQLUp.Set(1)
	e.metrics.Error.Set(0)
	e.metrics.CollectionDuration.WithLabelValues("collect.up").Set(time.Since(collectionStart).Seconds())

	var wg sync.WaitGroup
	defer wg.Wait()
	for _, pgCollector := range e.pgCollectors {
		wg.Add(1)
		go func(pgCollector PgCollector) {
			defer wg.Done()
			label := "collect." + pgCollector.name()
			collectionStart := time.Now()
			if err := pgCollector.collect(ch); err != nil {
				log.Log.Error(err, "Error during collection", "collector", pgCollector.name())
				e.metrics.PgCollectionErrors.WithLabelValues(label).Inc()
				e.metrics.Error.Set(1)
			}
			e.metrics.CollectionDuration.WithLabelValues(label).Set(time.Since(collectionStart).Seconds())
		}(pgCollector)
	}
}

// PgCollector is the interface for all the collectors that need to do queries
// on PostgreSQL to gather the results
type PgCollector interface {
	// name is the unique name of the collector
	name() string

	// collect collects data and send the metrics on the channel
	collect(ch chan<- prometheus.Metric) error
}
