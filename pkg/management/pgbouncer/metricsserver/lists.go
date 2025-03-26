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

// ShowListsMetrics contains all the SHOW LISTS Metrics
type ShowListsMetrics map[string]prometheus.Gauge

// Describe produces the description for all the contained Metrics
func (s ShowListsMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range s {
		m.Describe(ch)
	}
}

// Reset resets all the contained Metrics
func (s ShowListsMetrics) Reset() {
	for _, m := range s {
		m.Set(-1)
	}
}

// NewShowListsMetrics builds the default ShowListsMetrics
func NewShowListsMetrics(subsystem string) ShowListsMetrics {
	subsystem += "_lists"
	return ShowListsMetrics{
		"databases": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "databases",
			Help:      "Count of databases.",
		}),
		"users": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "users",
			Help:      "Count of users.",
		}),
		"pools": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "pools",
			Help:      "Count of pools.",
		}),
		"free_clients": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "free_clients",
			Help:      "Count of free clients.",
		}),
		"used_clients": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "used_clients",
			Help:      "Count of used clients.",
		}),
		"login_clients": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "login_clients",
			Help:      "Count of clients in login state.",
		}),
		"free_servers": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "free_servers",
			Help:      "Count of free servers.",
		}),
		"used_servers": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "used_servers",
			Help:      "Count of used servers.",
		}),
		"dns_names": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "dns_names",
			Help:      "Count of DNS names in the cache.",
		}),
		"dns_zones": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "dns_zones",
			Help:      "Count of DNS zones in the cache.",
		}),
		"dns_queries": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "dns_queries",
			Help:      "Count of in-flight DNS queries.",
		}),
		"dns_pending": prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "dns_pending",
			Help:      "Not used.",
		}),
	}
}

func (e *Exporter) collectShowLists(ch chan<- prometheus.Metric, db *sql.DB) {
	contextLogger := log.FromContext(e.ctx)

	e.Metrics.ShowLists.Reset()
	// First, let's check the connection. No need to proceed if this fails.
	rows, err := db.Query("SHOW LISTS;")
	if err != nil {
		contextLogger.Error(err, "Error while executing SHOW LISTS")
		e.Metrics.PgbouncerUp.Set(0)
		e.Metrics.Error.Set(1)
		return
	}

	e.Metrics.PgbouncerUp.Set(1)
	e.Metrics.Error.Set(0)
	defer func() {
		err = rows.Close()
		if err != nil {
			contextLogger.Error(err, "while closing rows for SHOW LISTS")
		}
	}()

	var (
		list string
		item int
	)

	for rows.Next() {
		if err = rows.Scan(&list, &item); err != nil {
			contextLogger.Error(err, "Error while executing SHOW LISTS")
			e.Metrics.Error.Set(1)
			e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
		}
		m, ok := e.Metrics.ShowLists[list]
		if !ok {
			e.Metrics.Error.Set(1)
			contextLogger.Info("Missing metric", "query", "SHOW LISTS", "metric", list)
			continue
		}
		m.Set(float64(item))
	}

	for _, m := range e.Metrics.ShowLists {
		m.Collect(ch)
	}

	if err = rows.Err(); err != nil {
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
	}
}
