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

package metricsserver

import (
	"database/sql"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
)

// ShowStatsMetrics contains all the SHOW STATS Metrics
type ShowStatsMetrics struct {
	TotalBindCount,
	TotalClientParseCount,
	TotalServerAssignCount,
	TotalServerParseCount,
	TotalXactCount,
	TotalQueryCount,
	TotalReceived,
	TotalSent,
	TotalXactTime,
	TotalQueryTime,
	TotalWaitTime,
	AvgBindCount,
	AvgClientParseCount,
	AvgServerAssignCount,
	AvgServerParseCount,
	AvgXactCount,
	AvgQueryCount,
	AvgRecv,
	AvgSent,
	AvgXactTime,
	AvgQueryTime,
	AvgWaitTime *prometheus.GaugeVec
}

// Describe produces the description for all the contained Metrics
func (r *ShowStatsMetrics) Describe(ch chan<- *prometheus.Desc) {
	r.TotalBindCount.Describe(ch)
	r.TotalClientParseCount.Describe(ch)
	r.TotalServerAssignCount.Describe(ch)
	r.TotalServerParseCount.Describe(ch)
	r.TotalXactCount.Describe(ch)
	r.TotalQueryCount.Describe(ch)
	r.TotalReceived.Describe(ch)
	r.TotalSent.Describe(ch)
	r.TotalXactTime.Describe(ch)
	r.TotalQueryTime.Describe(ch)
	r.TotalWaitTime.Describe(ch)
	r.AvgBindCount.Describe(ch)
	r.AvgClientParseCount.Describe(ch)
	r.AvgServerAssignCount.Describe(ch)
	r.AvgServerParseCount.Describe(ch)
	r.AvgXactCount.Describe(ch)
	r.AvgQueryCount.Describe(ch)
	r.AvgRecv.Describe(ch)
	r.AvgSent.Describe(ch)
	r.AvgXactTime.Describe(ch)
	r.AvgQueryTime.Describe(ch)
	r.AvgWaitTime.Describe(ch)
}

// Reset resets all the contained Metrics
func (r *ShowStatsMetrics) Reset() {
	r.TotalBindCount.Reset()
	r.TotalClientParseCount.Reset()
	r.TotalServerAssignCount.Reset()
	r.TotalServerParseCount.Reset()
	r.TotalXactCount.Reset()
	r.TotalQueryCount.Reset()
	r.TotalReceived.Reset()
	r.TotalSent.Reset()
	r.TotalXactTime.Reset()
	r.TotalQueryTime.Reset()
	r.TotalWaitTime.Reset()
	r.AvgBindCount.Reset()
	r.AvgClientParseCount.Reset()
	r.AvgServerAssignCount.Reset()
	r.AvgServerParseCount.Reset()
	r.AvgXactCount.Reset()
	r.AvgQueryCount.Reset()
	r.AvgRecv.Reset()
	r.AvgSent.Reset()
	r.AvgXactTime.Reset()
	r.AvgQueryTime.Reset()
	r.AvgWaitTime.Reset()
}

// NewShowStatsMetrics builds the default ShowStatsMetrics
func NewShowStatsMetrics(subsystem string) *ShowStatsMetrics {
	subsystem += "_stats"
	return &ShowStatsMetrics{
		TotalBindCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_bind_count",
			Help: "Total number of prepared statements readied for execution by clients and forwarded to " +
				"PostgreSQL by pgbouncer",
		}, []string{databaseLabel}),
		TotalClientParseCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_client_parse_count",
			Help:      "Total number of prepared statements created by clients.",
		}, []string{databaseLabel}),
		TotalServerAssignCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_server_assignment_count",
			Help:      "Total time a server was assigned to a client.",
		}, []string{databaseLabel}),
		TotalServerParseCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_server_parse_count",
			Help:      "Total number of prepared statements created by pgbouncer on a server.",
		}, []string{databaseLabel}),
		TotalXactCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_xact_count",
			Help:      "Total number of SQL transactions pooled by pgbouncer.",
		}, []string{databaseLabel}),
		TotalQueryCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_query_count",
			Help:      "Total number of SQL queries pooled by pgbouncer.",
		}, []string{databaseLabel}),
		TotalReceived: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_received",
			Help:      "Total volume in bytes of network traffic received by pgbouncer.",
		}, []string{databaseLabel}),
		TotalSent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_sent",
			Help:      "Total volume in bytes of network traffic sent by pgbouncer.",
		}, []string{databaseLabel}),
		TotalXactTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_xact_time",
			Help: "Total number of microseconds spent by pgbouncer when connected to PostgreSQL " +
				"in a transaction, either idle in transaction or executing queries.",
		}, []string{databaseLabel}),
		TotalQueryTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_query_time",
			Help: "Total number of microseconds spent by pgbouncer when actively connected " +
				"to PostgreSQL, executing queries.",
		}, []string{databaseLabel}),
		TotalWaitTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_wait_time",
			Help:      "Time spent by clients waiting for a server, in microseconds.",
		}, []string{databaseLabel}),
		AvgBindCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_bind_count",
			Help: "Average number of prepared statements readied for execution by clients and forwarded to " +
				"PostgreSQL by pgbouncer.",
		}, []string{databaseLabel}),
		AvgClientParseCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_client_parse_count",
			Help:      "Average number of prepared statements created by clients.",
		}, []string{databaseLabel}),
		AvgServerAssignCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_server_assignment_count",
			Help: "Average number of times a server was assigned to a client per second in " +
				"the last stat period.",
		}, []string{databaseLabel}),
		AvgServerParseCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_server_parse_count",
			Help:      "Average number of prepared statements created by pgbouncer on a server.",
		}, []string{databaseLabel}),
		AvgXactCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_xact_count",
			Help:      "Average transactions per second in last stat period.",
		}, []string{databaseLabel}),
		AvgQueryCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_query_count",
			Help:      "Average queries per second in last stat period.",
		}, []string{databaseLabel}),
		AvgRecv: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_recv",
			Help:      "Average received (from clients) bytes per second.",
		}, []string{databaseLabel}),
		AvgSent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_sent",
			Help:      "Average sent (to clients) bytes per second.",
		}, []string{databaseLabel}),
		AvgXactTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_xact_time",
			Help:      "Average transaction duration, in microseconds.",
		}, []string{databaseLabel}),
		AvgQueryTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_query_time",
			Help:      "Average query duration, in microseconds.",
		}, []string{databaseLabel}),
		AvgWaitTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_wait_time",
			Help:      "Time spent by clients waiting for a server, in microseconds (average per second).",
		}, []string{databaseLabel}),
	}
}

func (r *ShowStatsMetrics) byColumn() map[string]*prometheus.GaugeVec {
	return map[string]*prometheus.GaugeVec{
		"total_bind_count":              r.TotalBindCount,
		"total_client_parse_count":      r.TotalClientParseCount,
		"total_server_assignment_count": r.TotalServerAssignCount,
		"total_server_parse_count":      r.TotalServerParseCount,
		"total_xact_count":              r.TotalXactCount,
		"total_query_count":             r.TotalQueryCount,
		"total_received":                r.TotalReceived,
		"total_sent":                    r.TotalSent,
		"total_xact_time":               r.TotalXactTime,
		"total_query_time":              r.TotalQueryTime,
		"total_wait_time":               r.TotalWaitTime,
		"avg_bind_count":                r.AvgBindCount,
		"avg_client_parse_count":        r.AvgClientParseCount,
		"avg_server_assignment_count":   r.AvgServerAssignCount,
		"avg_server_parse_count":        r.AvgServerParseCount,
		"avg_xact_count":                r.AvgXactCount,
		"avg_query_count":               r.AvgQueryCount,
		"avg_recv":                      r.AvgRecv,
		"avg_sent":                      r.AvgSent,
		"avg_xact_time":                 r.AvgXactTime,
		"avg_query_time":                r.AvgQueryTime,
		"avg_wait_time":                 r.AvgWaitTime,
	}
}

// Collect produces the values for all the contained Metrics
func (r *ShowStatsMetrics) Collect(ch chan<- prometheus.Metric) {
	for _, gauge := range r.byColumn() {
		gauge.Collect(ch)
	}
}

func (e *Exporter) collectShowStats(ch chan<- prometheus.Metric, db *sql.DB) {
	contextLogger := log.FromContext(e.ctx)

	e.Metrics.ShowStats.Reset()
	// First, let's check the connection. No need to proceed if this fails.
	rows, err := db.Query("SHOW STATS;")
	if err != nil {
		contextLogger.Error(err, "Error while executing SHOW STATS")
		e.Metrics.PgbouncerUp.Set(0)
		e.Metrics.Error.Set(1)
		return
	}

	e.Metrics.PgbouncerUp.Set(1)
	e.Metrics.Error.Set(0)
	defer func() {
		err = rows.Close()
		if err != nil {
			contextLogger.Error(err, "while closing rows for SHOW STATS")
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		contextLogger.Error(err, "Error while reading SHOW STATS")
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
		return
	}

	gauges := e.Metrics.ShowStats.byColumn()
	var database string
	values := make([]int, len(columns))
	targets := make([]any, len(columns))
	for i, column := range columns {
		switch {
		case column == databaseLabel:
			targets[i] = &database
		case gauges[column] != nil:
			targets[i] = &values[i]
		default:
			targets[i] = new(any)
		}
	}

	for rows.Next() {
		if err := rows.Scan(targets...); err != nil {
			contextLogger.Error(err, "Error while executing SHOW STATS")
			e.Metrics.Error.Set(1)
			e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
			continue
		}
		for i, column := range columns {
			if gauge := gauges[column]; gauge != nil {
				gauge.WithLabelValues(database).Set(float64(values[i]))
			}
		}
	}

	e.Metrics.ShowStats.Collect(ch)

	if err = rows.Err(); err != nil {
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
	}
}
