/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

const pgStatArchiverCollectorName = "pg_stat_archiver"

// pgStatArchiverCollector define the exported metrics and the instance
// we extract them from
type pgStatArchiverCollector struct {
	archivedCount *prometheus.Desc
	failedCount   *prometheus.Desc
	instance      *postgres.Instance
}

// confirm we respect the interface
var _ PgCollector = pgStatArchiverCollector{}

// newPgStatArchiverCollector create a new pgStatArchiverCollector
func newPgStatArchiverCollector(instance *postgres.Instance) PgCollector {
	return &pgStatArchiverCollector{
		archivedCount: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				pgStatArchiverCollectorName,
				"archived_count"),
			"Number of WAL files that have been successfully archived",
			nil, nil),
		failedCount: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				pgStatArchiverCollectorName,
				"failed_count"),
			"Number of failed attempts for archiving WAL files",
			nil, nil),
		instance: instance,
	}
}

// name returns the name of the collector. Implements PgCollector
func (pgStatArchiverCollector) Name() string {
	return pgStatArchiverCollectorName
}

// collect send the collected metrics on the received channel.
// Implements PgCollector
func (c pgStatArchiverCollector) Collect(ch chan<- prometheus.Metric) error {
	conn, err := c.instance.GetApplicationDB()
	if err != nil {
		return err
	}
	pgStatArchiverRow := conn.QueryRow(
		"SELECT archived_count, failed_count FROM pg_stat_archiver")
	var (
		archivedCount int64
		failedCount   int64
	)
	if err := pgStatArchiverRow.Scan(&archivedCount, &failedCount); err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(
		c.archivedCount, prometheus.CounterValue, float64(archivedCount))
	ch <- prometheus.MustNewConstMetric(
		c.failedCount, prometheus.CounterValue, float64(failedCount))

	return nil
}

// Describe puts on the channel the metadata about the metrics we collect
func (c pgStatArchiverCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.archivedCount
	ch <- c.failedCount
}
