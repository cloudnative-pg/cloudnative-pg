/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/postgres"
)

const pgLocksCollectorName = "pg_locks"

// pgLocksCollector define the exported metrics and the instance
// we extract them from
type pgLocksCollector struct {
	waitingBackends prometheus.Gauge
	instance        *postgres.Instance
}

// confirm we respect the interface
var _ PgCollector = pgLocksCollector{}

// newPgLocksCollector create a new pgLocksCollector
func newPgLocksCollector(instance *postgres.Instance) PgCollector {
	return &pgLocksCollector{
		waitingBackends: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: pgLocksCollectorName,
			Name:      "backends_waiting",
			Help:      "Number of client backends waiting",
		}),
		instance: instance,
	}
}

// name returns the name of the collector. Implements PgCollector
func (pgLocksCollector) name() string {
	return pgLocksCollectorName
}

// collect send the collected metrics on the received channel.
// Implements PgCollector
func (c pgLocksCollector) collect(ch chan<- prometheus.Metric) error {
	conn, err := c.instance.GetApplicationDB()
	if err != nil {
		return err
	}

	waitingBackendsRow := conn.QueryRow(`
    SELECT count(*)
    FROM pg_catalog.pg_locks         blocked_locks
    JOIN pg_catalog.pg_locks         blocking_locks
        ON blocking_locks.locktype = blocked_locks.locktype
        AND blocking_locks.database IS NOT DISTINCT FROM blocked_locks.database
        AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
        AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page
        AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple
        AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid
        AND blocking_locks.transactionid IS NOT DISTINCT FROM blocked_locks.transactionid
        AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid
        AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid
        AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid
        AND blocking_locks.pid != blocked_locks.pid
    JOIN pg_catalog.pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid
    WHERE NOT blocked_locks.granted;
	`)

	var waitingBackends int64
	if err := waitingBackendsRow.Scan(&waitingBackends); err != nil {
		return err
	}
	c.waitingBackends.Set(float64(waitingBackends))
	c.waitingBackends.Collect(ch)
	return nil
}
