/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

const pgStatReplicationCollectorName = "pg_stat_replication"

// pgStatReplicationCollector define the exported metrics and the instance
// we extract them from
type pgStatReplicationCollector struct {
	writeLagSeconds  *prometheus.GaugeVec
	flushLagSeconds  *prometheus.GaugeVec
	replayLagSeconds *prometheus.GaugeVec
	sentDiffBytes    *prometheus.GaugeVec
	writeDiffBytes   *prometheus.GaugeVec
	flushDiffBytes   *prometheus.GaugeVec
	replayDiffBytes  *prometheus.GaugeVec
	instance         *postgres.Instance
}

// confirm we respect the interface
var _ PgCollector = pgStatReplicationCollector{}

// newPgStatReplicationCollector create a new pgStatReplicationCollector
func newPgStatReplicationCollector(instance *postgres.Instance) PgCollector {
	return &pgStatReplicationCollector{
		writeLagSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "write_lag_seconds",
			Help: "Time elapsed between flushing recent WAL locally and " +
				"receiving notification that this standby server has " +
				"written it (but not yet flushed it or applied it)",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		flushLagSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "flush_lag_seconds",
			Help: "Time elapsed between flushing recent WAL locally and " +
				"receiving notification that this standby server has " +
				"written and flushed it (but not yet applied it)",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		replayLagSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "replay_lag_seconds",
			Help: "Time elapsed between flushing recent WAL locally and " +
				"receiving notification that this standby server has " +
				"written, flushed and applied it",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		sentDiffBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "sent_lag_bytes",
			Help: "Difference in bytes between the local current lsn and the " +
				"one sent to the standby server",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		writeDiffBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "write_lag_bytes",
			Help: "Difference in bytes between the local current lsn and the " +
				"one written on the standby server",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		flushDiffBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "flush_lag_bytes",
			Help: "Difference in bytes between the local current lsn and the " +
				"one flushed on the standby server",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		replayDiffBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgStatReplicationCollectorName,
			Name:      "replay_lag_bytes",
			Help: "Difference in bytes between the local current lsn and the " +
				"one replayed on the standby server",
		},
			[]string{"usename", "application_name", "client_addr"},
		),
		instance: instance,
	}
}

// name returns the name of the collector. Implements PgCollector
func (pgStatReplicationCollector) name() string {
	return pgStatReplicationCollectorName
}

// collect send the collected metrics on the received channel.
// Implements PgCollector
func (c pgStatReplicationCollector) collect(ch chan<- prometheus.Metric) error {
	conn, err := c.instance.GetApplicationDB()
	if err != nil {
		return err
	}
	pgStatReplicationRows, err := conn.Query(`
	SELECT usename
        , COALESCE(application_name, '')
		, COALESCE(client_addr::text, '') AS client_addr
        , pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn) AS sent_diff_bytes
        , pg_wal_lsn_diff(pg_current_wal_lsn(), write_lsn) AS write_diff_bytes
        , pg_wal_lsn_diff(pg_current_wal_lsn(), flush_lsn) AS flush_diff_bytes
        , COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn),0) AS replay_diff_bytes
        , COALESCE((EXTRACT(EPOCH FROM write_lag)),0)::float AS write_lag_seconds
        , COALESCE((EXTRACT(EPOCH FROM flush_lag)),0)::float AS flush_lag_seconds
        , COALESCE((EXTRACT(EPOCH FROM replay_lag)),0)::float AS replay_lag_seconds
    FROM pg_stat_replication;
	`)

	if err != nil {
		return err
	}

	for pgStatReplicationRows.Next() {
		var (
			usename          string
			applicationName  string
			clientAddr       string
			sentDiffBytes    int64
			writeDiffBytes   int64
			flushDiffBytes   int64
			replayDiffBytes  int64
			writeLagSeconds  float64
			flushLagSeconds  float64
			replayLagSeconds float64
		)
		if err := pgStatReplicationRows.Scan(
			&usename,
			&applicationName,
			&clientAddr,
			&sentDiffBytes,
			&writeDiffBytes,
			&flushDiffBytes,
			&replayDiffBytes,
			&writeLagSeconds,
			&flushLagSeconds,
			&replayLagSeconds,
		); err != nil {
			return err
		}
		m := c.sentDiffBytes.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(float64(sentDiffBytes))
		m.Collect(ch)
		m = c.writeDiffBytes.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(float64(writeDiffBytes))
		m.Collect(ch)
		m = c.flushDiffBytes.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(float64(flushDiffBytes))
		m.Collect(ch)
		m = c.replayDiffBytes.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(float64(replayDiffBytes))
		m.Collect(ch)
		m = c.writeLagSeconds.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(writeLagSeconds)
		m.Collect(ch)
		m = c.flushLagSeconds.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(flushLagSeconds)
		m.Collect(ch)
		m = c.replayLagSeconds.WithLabelValues(usename,
			applicationName, clientAddr)
		m.Set(replayLagSeconds)
		m.Collect(ch)
	}

	return nil
}
