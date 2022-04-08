/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metricsserver

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// ShowStatsMetrics contains all the SHOW STATS Metrics
type ShowStatsMetrics struct {
	TotalXactCount,
	TotalQueryCount,
	TotalReceived,
	TotalSent,
	TotalXactTime,
	TotalQueryTime,
	TotalWaitTime,
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
	r.TotalXactCount.Describe(ch)
	r.TotalQueryCount.Describe(ch)
	r.TotalReceived.Describe(ch)
	r.TotalSent.Describe(ch)
	r.TotalXactTime.Describe(ch)
	r.TotalQueryTime.Describe(ch)
	r.TotalWaitTime.Describe(ch)
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
	r.TotalXactCount.Reset()
	r.TotalQueryCount.Reset()
	r.TotalReceived.Reset()
	r.TotalSent.Reset()
	r.TotalXactTime.Reset()
	r.TotalQueryTime.Reset()
	r.TotalWaitTime.Reset()
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
		TotalXactCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_xact_count",
			Help:      "Total number of SQL transactions pooled by pgbouncer.",
		}, []string{"database"}),
		TotalQueryCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_query_count",
			Help:      "Total number of SQL queries pooled by pgbouncer.",
		}, []string{"database"}),
		TotalReceived: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_received",
			Help:      "Total volume in bytes of network traffic received by pgbouncer.",
		}, []string{"database"}),
		TotalSent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_sent",
			Help:      "Total volume in bytes of network traffic sent by pgbouncer.",
		}, []string{"database"}),
		TotalXactTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_xact_time",
			Help: "Total number of microseconds spent by pgbouncer when connected to PostgreSQL " +
				"in a transaction, either idle in transaction or executing queries.",
		}, []string{"database"}),
		TotalQueryTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_query_time",
			Help: "Total number of microseconds spent by pgbouncer when actively connected " +
				"to PostgreSQL, executing queries.",
		}, []string{"database"}),
		TotalWaitTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "total_wait_time",
			Help:      "Time spent by clients waiting for a server, in microseconds.",
		}, []string{"database"}),
		AvgXactCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_xact_count",
			Help:      "Average transactions per second in last stat period.",
		}, []string{"database"}),
		AvgQueryCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_query_count",
			Help:      "Average queries per second in last stat period.",
		}, []string{"database"}),
		AvgRecv: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_recv",
			Help:      "Average received (from clients) bytes per second.",
		}, []string{"database"}),
		AvgSent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_sent",
			Help:      "Average sent (to clients) bytes per second.",
		}, []string{"database"}),
		AvgXactTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_xact_time",
			Help:      "Average transaction duration, in microseconds.",
		}, []string{"database"}),
		AvgQueryTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_query_time",
			Help:      "Average query duration, in microseconds.",
		}, []string{"database"}),
		AvgWaitTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "avg_wait_time",
			Help:      "Time spent by clients waiting for a server, in microseconds (average per second).",
		}, []string{"database"}),
	}
}

func (e *Exporter) collectShowStats(ch chan<- prometheus.Metric, db *sql.DB) {
	e.Metrics.ShowStats.Reset()
	// First, let's check the connection. No need to proceed if this fails.
	rows, err := db.Query("SHOW STATS;")
	if err != nil {
		log.Error(err, "Error while executing SHOW STATS")
		e.Metrics.PgbouncerUp.Set(0)
		e.Metrics.Error.Set(1)
		return
	}

	e.Metrics.PgbouncerUp.Set(1)
	e.Metrics.Error.Set(0)
	defer func() {
		err = rows.Close()
		if err != nil {
			log.Error(err, "while closing rows for SHOW STATS")
		}
	}()
	var (
		database string
		totalXactCount,
		totalQueryCount,
		totalReceived,
		totalSent,
		totalXactTime,
		totalQueryTime,
		totalWaitTime,
		avgXactCount,
		avgQueryCount,
		avgRecv,
		avgSent,
		avgXactTime,
		avgQueryTime,
		avgWaitTime int
	)

	for rows.Next() {
		if err = rows.Scan(&database,
			&totalXactCount,
			&totalQueryCount,
			&totalReceived,
			&totalSent,
			&totalXactTime,
			&totalQueryTime,
			&totalWaitTime,
			&avgXactCount,
			&avgQueryCount,
			&avgRecv,
			&avgSent,
			&avgXactTime,
			&avgQueryTime,
			&avgWaitTime,
		); err != nil {
			log.Error(err, "Error while executing SHOW STATS")
			e.Metrics.Error.Set(1)
			e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
		}
		e.Metrics.ShowStats.TotalXactCount.WithLabelValues(database).Set(float64(totalXactCount))
		e.Metrics.ShowStats.TotalQueryCount.WithLabelValues(database).Set(float64(totalQueryCount))
		e.Metrics.ShowStats.TotalReceived.WithLabelValues(database).Set(float64(totalReceived))
		e.Metrics.ShowStats.TotalSent.WithLabelValues(database).Set(float64(totalSent))
		e.Metrics.ShowStats.TotalXactTime.WithLabelValues(database).Set(float64(totalXactTime))
		e.Metrics.ShowStats.TotalQueryTime.WithLabelValues(database).Set(float64(totalQueryTime))
		e.Metrics.ShowStats.TotalWaitTime.WithLabelValues(database).Set(float64(totalWaitTime))
		e.Metrics.ShowStats.AvgXactCount.WithLabelValues(database).Set(float64(avgXactCount))
		e.Metrics.ShowStats.AvgQueryCount.WithLabelValues(database).Set(float64(avgQueryCount))
		e.Metrics.ShowStats.AvgRecv.WithLabelValues(database).Set(float64(avgRecv))
		e.Metrics.ShowStats.AvgSent.WithLabelValues(database).Set(float64(avgSent))
		e.Metrics.ShowStats.AvgXactTime.WithLabelValues(database).Set(float64(avgXactTime))
		e.Metrics.ShowStats.AvgQueryTime.WithLabelValues(database).Set(float64(avgQueryTime))
		e.Metrics.ShowStats.AvgWaitTime.WithLabelValues(database).Set(float64(avgWaitTime))
	}

	e.Metrics.ShowStats.TotalXactCount.Collect(ch)
	e.Metrics.ShowStats.TotalQueryCount.Collect(ch)
	e.Metrics.ShowStats.TotalReceived.Collect(ch)
	e.Metrics.ShowStats.TotalSent.Collect(ch)
	e.Metrics.ShowStats.TotalXactTime.Collect(ch)
	e.Metrics.ShowStats.TotalQueryTime.Collect(ch)
	e.Metrics.ShowStats.TotalWaitTime.Collect(ch)
	e.Metrics.ShowStats.AvgXactCount.Collect(ch)
	e.Metrics.ShowStats.AvgQueryCount.Collect(ch)
	e.Metrics.ShowStats.AvgRecv.Collect(ch)
	e.Metrics.ShowStats.AvgSent.Collect(ch)
	e.Metrics.ShowStats.AvgXactTime.Collect(ch)
	e.Metrics.ShowStats.AvgQueryTime.Collect(ch)
	e.Metrics.ShowStats.AvgWaitTime.Collect(ch)

	if err = rows.Err(); err != nil {
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
	}
}
