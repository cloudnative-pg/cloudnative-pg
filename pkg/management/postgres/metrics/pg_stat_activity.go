/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

const pgStatActivityCollectorName = "pg_stat_activity"

// pgStatArchiverCollector define the exported metrics and the instance
// we extract them from
type pgStatActivityCollector struct {
	backends             *prometheus.GaugeVec
	maxTxDurationSeconds *prometheus.GaugeVec
	instance             *postgres.Instance
}

// confirm we respect the interface
var _ PgCollector = pgStatActivityCollector{}

// newPgStatArchiverCollector create a new pgStatArchiverCollector
func newPgStatActivityCollector(instance *postgres.Instance) PgCollector {
	return &pgStatActivityCollector{
		backends: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatActivityCollectorName,
			Name:      "backends",
			Help:      "Number of open backends",
		},
			[]string{"state", "usename"},
		),
		maxTxDurationSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatActivityCollectorName,
			Name:      "tx_max_duration_seconds",
			Help:      "Duration of the longest running transaction in seconds",
		},
			[]string{"state", "usename"},
		),
		instance: instance,
	}
}

// name returns the name of the collector. Implements PgCollector
func (pgStatActivityCollector) name() string {
	return pgStatActivityCollectorName
}

// collect send the collected metrics on the received channel.
// Implements PgCollector
func (c pgStatActivityCollector) collect(ch chan<- prometheus.Metric) error {
	conn, err := c.instance.GetApplicationDB()
	if err != nil {
		return err
	}
	pgStatActivityRows, err := conn.Query(`
	SELECT states.state
        , sa.usename
        , COALESCE(sa.count, 0) AS backends
        , COALESCE(sa.max_tx_secs, 0) AS max_tx_duration_seconds
	FROM ( VALUES ('active')
        , ('idle')
        , ('idle in transaction')
        , ('idle in transaction (aborted)')
        , ('fastpath function call')
        , ('disabled')
    ) AS states(state)
    LEFT JOIN (
        SELECT state
            , usename
            , count(*)
            , COALESCE(EXTRACT (EPOCH FROM (max(now() - xact_start))), 0) AS max_tx_secs
        FROM pg_stat_activity
        GROUP BY state, usename
	) sa ON states.state = sa.state
	WHERE sa.usename IS NOT NULL;
	`)

	if err != nil {
		return err
	}

	for pgStatActivityRows.Next() {
		var (
			state                string
			usename              string
			backends             int64
			maxTxDurationSeconds float64
		)
		if err := pgStatActivityRows.Scan(&state, &usename, &backends, &maxTxDurationSeconds); err != nil {
			return err
		}
		m := c.backends.WithLabelValues(state, usename)
		m.Set(float64(backends))
		m.Collect(ch)
		m = c.maxTxDurationSeconds.WithLabelValues(state, usename)
		m.Set(maxTxDurationSeconds)
		m.Collect(ch)
	}

	return nil
}
