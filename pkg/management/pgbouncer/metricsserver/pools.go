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

// ShowPoolsMetrics contains all the SHOW POOLS Metrics
type ShowPoolsMetrics struct {
	ClActive,
	ClWaiting,
	ClCancelReq,
	ClActiveCancelReq,
	ClWaitingCancelReq,
	SvActive,
	SvActiveCancel,
	SvBeingCanceled,
	SvIdle,
	SvUsed,
	SvTested,
	SvLogin,
	MaxWait,
	MaxWaitUs,
	PoolMode,
	LoadBalanceHosts *prometheus.GaugeVec
}

// byColumn maps every SHOW POOLS column to its gauge. It is the single source of truth for the
// metrics of this collector: Describe, Reset, Collect and the row scan all derive from it.
func (r *ShowPoolsMetrics) byColumn() map[string]*prometheus.GaugeVec {
	return map[string]*prometheus.GaugeVec{
		"cl_active":             r.ClActive,
		"cl_waiting":            r.ClWaiting,
		"cl_cancel_req":         r.ClCancelReq,
		"cl_active_cancel_req":  r.ClActiveCancelReq,
		"cl_waiting_cancel_req": r.ClWaitingCancelReq,
		"sv_active":             r.SvActive,
		"sv_active_cancel":      r.SvActiveCancel,
		"sv_being_canceled":     r.SvBeingCanceled,
		"sv_idle":               r.SvIdle,
		"sv_used":               r.SvUsed,
		"sv_tested":             r.SvTested,
		"sv_login":              r.SvLogin,
		"maxwait":               r.MaxWait,
		"maxwait_us":            r.MaxWaitUs,
		"pool_mode":             r.PoolMode,
		"load_balance_hosts":    r.LoadBalanceHosts,
	}
}

// Describe produces the description for all the contained Metrics
func (r *ShowPoolsMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, gauge := range r.byColumn() {
		gauge.Describe(ch)
	}
}

// Reset resets all the contained Metrics
func (r *ShowPoolsMetrics) Reset() {
	for _, gauge := range r.byColumn() {
		gauge.Reset()
	}
}

// Collect produces the values for all the contained Metrics
func (r *ShowPoolsMetrics) Collect(ch chan<- prometheus.Metric) {
	for _, gauge := range r.byColumn() {
		gauge.Collect(ch)
	}
}

// NewShowPoolsMetrics builds the default ShowPoolsMetrics
func NewShowPoolsMetrics(subsystem string) *ShowPoolsMetrics {
	subsystem += "_pools"
	return &ShowPoolsMetrics{
		ClActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_active",
			Help:      "Client connections that are linked to server connection and can process queries.",
		}, []string{databaseLabel, userLabel}),
		ClWaiting: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_waiting",
			Help:      "Client connections that have sent queries but have not yet got a server connection.",
		}, []string{databaseLabel, userLabel}),
		ClCancelReq: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_cancel_req",
			Help:      "Client connections that have not forwarded query cancellations to the server yet.",
		}, []string{databaseLabel, userLabel}),
		ClActiveCancelReq: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_active_cancel_req",
			Help: "Client connections that have forwarded query cancellations to the server and " +
				"are waiting for the server response.",
		}, []string{databaseLabel, userLabel}),
		ClWaitingCancelReq: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_waiting_cancel_req",
			Help:      "Client connections that have not forwarded query cancellations to the server yet.",
		}, []string{databaseLabel, userLabel}),
		SvActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_active",
			Help:      "Server connections that are linked to a client.",
		}, []string{databaseLabel, userLabel}),
		SvActiveCancel: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_active_cancel",
			Help:      "Server connections that are currently forwarding a cancel request",
		}, []string{databaseLabel, userLabel}),
		SvBeingCanceled: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_wait_cancels",
			Help: "Servers that normally could become idle, but are waiting to do so until all in-flight cancel " +
				"requests have completed that were sent to cancel a query on this server.",
		}, []string{databaseLabel, userLabel}),
		SvIdle: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_idle",
			Help:      "Server connections that are unused and immediately usable for client queries.",
		}, []string{databaseLabel, userLabel}),
		SvUsed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_used",
			Help: "Server connections that have been idle for more than server_check_delay, so they need " +
				"server_check_query to run on them before they can be used again.",
		}, []string{databaseLabel, userLabel}),
		SvTested: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_tested",
			Help: "Server connections that are currently running either server_reset_query or " +
				"server_check_query.",
		}, []string{databaseLabel, userLabel}),
		SvLogin: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_login",
			Help:      "Server connections currently in the process of logging in.",
		}, []string{databaseLabel, userLabel}),
		MaxWait: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "maxwait",
			Help: "How long the first (oldest) client in the queue has waited, in seconds. If this starts " +
				"increasing, then the current pool of servers does not handle requests quickly enough. The " +
				"reason may be either an overloaded server or just too small of a pool_size setting.",
		}, []string{databaseLabel, userLabel}),
		MaxWaitUs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "maxwait_us",
			Help:      "Microsecond part of the maximum waiting time.",
		}, []string{databaseLabel, userLabel}),
		PoolMode: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "pool_mode",
			Help:      "The pooling mode in use. 1 for session, 2 for transaction, 3 for statement, -1 if unknown",
		}, []string{databaseLabel, userLabel}),
		LoadBalanceHosts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "load_balance_hosts",
			Help: "The host load balancing mode in use. 1 for disable, 2 for round-robin, " +
				"0 when the pool has a single host, -1 if unknown",
		}, []string{databaseLabel, userLabel}),
	}
}

func (e *Exporter) collectShowPools(ch chan<- prometheus.Metric, db *sql.DB) {
	contextLogger := log.FromContext(e.ctx)

	e.Metrics.ShowPools.Reset()
	// First, let's check the connection. No need to proceed if this fails.
	rows, err := db.Query("SHOW POOLS;")
	if err != nil {
		contextLogger.Error(err, "Error while executing SHOW POOLS")
		e.Metrics.PgbouncerUp.Set(0)
		e.Metrics.Error.Set(1)
		return
	}

	e.Metrics.PgbouncerUp.Set(1)
	e.Metrics.Error.Set(0)
	defer func() {
		err = rows.Close()
		if err != nil {
			contextLogger.Error(err, "while closing rows for SHOW POOLS")
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		contextLogger.Error(err, "Error while getting number of columns")
		e.Metrics.PgbouncerUp.Set(0)
		e.Metrics.Error.Set(1)
		return
	}

	// PgBouncer adds and removes SHOW POOLS columns across releases, so bind each one to its gauge
	// by name and ignore the columns this version does not know about. pool_mode and
	// load_balance_hosts are reported as text (src/admin.c, admin_show_pools).
	gauges := e.Metrics.ShowPools.byColumn()
	var database, user string
	values := make([]int, len(columns))
	texts := make([]sql.NullString, len(columns))
	targets := make([]any, len(columns))
	for i, column := range columns {
		switch column {
		case databaseLabel:
			targets[i] = &database
		case userLabel:
			targets[i] = &user
		case "pool_mode", "load_balance_hosts":
			targets[i] = &texts[i]
		default:
			if gauges[column] != nil {
				targets[i] = &values[i]
			} else {
				targets[i] = new(any)
			}
		}
	}

	for rows.Next() {
		if err := rows.Scan(targets...); err != nil {
			contextLogger.Error(err, "Error while executing SHOW POOLS")
			e.Metrics.Error.Set(1)
			e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
			continue
		}
		for i, column := range columns {
			gauge := gauges[column]
			if gauge == nil {
				continue
			}
			switch column {
			case "pool_mode":
				gauge.WithLabelValues(database, user).Set(float64(poolModeToInt(texts[i].String)))
			case "load_balance_hosts":
				gauge.WithLabelValues(database, user).Set(float64(loadBalanceHostsToInt(texts[i])))
			default:
				gauge.WithLabelValues(database, user).Set(float64(values[i]))
			}
		}
	}

	e.Metrics.ShowPools.Collect(ch)

	if err = rows.Err(); err != nil {
		e.Metrics.Error.Set(1)
		e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
	}
}

func poolModeToInt(poolMode string) int {
	switch poolMode {
	case "session":
		return 1
	case "transaction":
		return 2
	case "statement":
		return 3
	default:
		return -1
	}
}

// loadBalanceHostsToInt encodes the load_balance_hosts values of src/main.c load_balance_hosts_map.
// PgBouncer reports the column as null when the pool has a single host, which is every pool
// CloudNativePG configures, so that case gets its own value rather than being folded into unknown.
func loadBalanceHostsToInt(loadBalanceHosts sql.NullString) int {
	if !loadBalanceHosts.Valid {
		return 0
	}
	switch loadBalanceHosts.String {
	case "disable":
		return 1
	case "round-robin":
		return 2
	default:
		return -1
	}
}
