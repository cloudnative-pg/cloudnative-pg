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

// Describe produces the description for all the contained Metrics
func (r *ShowPoolsMetrics) Describe(ch chan<- *prometheus.Desc) {
	r.ClActive.Describe(ch)
	r.ClWaiting.Describe(ch)
	r.ClCancelReq.Describe(ch)
	r.ClActiveCancelReq.Describe(ch)
	r.ClWaitingCancelReq.Describe(ch)
	r.SvActive.Describe(ch)
	r.SvActiveCancel.Describe(ch)
	r.SvBeingCanceled.Describe(ch)
	r.SvIdle.Describe(ch)
	r.SvUsed.Describe(ch)
	r.SvTested.Describe(ch)
	r.SvLogin.Describe(ch)
	r.MaxWait.Describe(ch)
	r.MaxWaitUs.Describe(ch)
	r.PoolMode.Describe(ch)
}

// Reset resets all the contained Metrics
func (r *ShowPoolsMetrics) Reset() {
	r.ClActive.Reset()
	r.ClWaiting.Reset()
	r.ClCancelReq.Reset()
	r.ClActiveCancelReq.Reset()
	r.ClWaitingCancelReq.Reset()
	r.SvActive.Reset()
	r.SvActiveCancel.Reset()
	r.SvBeingCanceled.Reset()
	r.SvIdle.Reset()
	r.SvUsed.Reset()
	r.SvTested.Reset()
	r.SvLogin.Reset()
	r.MaxWait.Reset()
	r.MaxWaitUs.Reset()
	r.PoolMode.Reset()
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
		}, []string{"database", "user"}),
		ClWaiting: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_waiting",
			Help:      "Client connections that have sent queries but have not yet got a server connection.",
		}, []string{"database", "user"}),
		ClCancelReq: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_cancel_req",
			Help:      "Client connections that have not forwarded query cancellations to the server yet.",
		}, []string{"database", "user"}),
		ClActiveCancelReq: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_active_cancel_req",
			Help: "Client connections that have forwarded query cancellations to the server and " +
				"are waiting for the server response.",
		}, []string{"database", "user"}),
		ClWaitingCancelReq: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "cl_waiting_cancel_req",
			Help:      "Client connections that have not forwarded query cancellations to the server yet.",
		}, []string{"database", "user"}),
		SvActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_active",
			Help:      "Server connections that are linked to a client.",
		}, []string{"database", "user"}),
		SvActiveCancel: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_active_cancel",
			Help:      "Server connections that are currently forwarding a cancel request",
		}, []string{"database", "user"}),
		SvBeingCanceled: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_wait_cancels",
			Help: "Servers that normally could become idle, but are waiting to do so until all in-flight cancel " +
				"requests have completed that were sent to cancel a query on this server.",
		}, []string{"database", "user"}),
		SvIdle: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_idle",
			Help:      "Server connections that are unused and immediately usable for client queries.",
		}, []string{"database", "user"}),
		SvUsed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_used",
			Help: "Server connections that have been idle for more than server_check_delay, so they need " +
				"server_check_query to run on them before they can be used again.",
		}, []string{"database", "user"}),
		SvTested: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_tested",
			Help: "Server connections that are currently running either server_reset_query or " +
				"server_check_query.",
		}, []string{"database", "user"}),
		SvLogin: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "sv_login",
			Help:      "Server connections currently in the process of logging in.",
		}, []string{"database", "user"}),
		MaxWait: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "maxwait",
			Help: "How long the first (oldest) client in the queue has waited, in seconds. If this starts " +
				"increasing, then the current pool of servers does not handle requests quickly enough. The " +
				"reason may be either an overloaded server or just too small of a pool_size setting.",
		}, []string{"database", "user"}),
		MaxWaitUs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "maxwait_us",
			Help:      "Microsecond part of the maximum waiting time.",
		}, []string{"database", "user"}),
		PoolMode: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "pool_mode",
			Help:      "The pooling mode in use. 1 for session, 2 for transaction, 3 for statement, -1 if unknown",
		}, []string{"database", "user"}),
		LoadBalanceHosts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: subsystem,
			Name:      "load_balance_hosts",
			Help:      "Number of hosts not load balancing between hosts",
		}, []string{"database", "user"}),
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

	// common columns
	var (
		database  string
		user      string
		clActive  int
		clWaiting int
		svActive  int
		svIdle    int
		svUsed    int
		svTested  int
		svLogin   int
		maxWait   int
		maxWaitUs int
		poolMode  string
	)

	// exclusive columns for 'version < 1.18.0'
	var (
		clCancelReq int
	)

	// PGBouncer 1.18.0 exclusive columns
	var (
		clActiveCancelReq  int
		clWaitingCancelReq int
		svActiveCancel     int
		svBeingCanceled    int
	)
	// PGBouncer 1.24.0 or above
	var (
		loadBalanceHosts sql.NullInt32
	)

	cols, err := rows.Columns()
	if err != nil {
		contextLogger.Error(err, "Error while getting number of columns")
		e.Metrics.PgbouncerUp.Set(0)
		e.Metrics.Error.Set(1)
		return
	}
	for rows.Next() {
		const (
			poolsColumnsPgBouncer1180 = 16
			poolsColumnsPgBouncer1240 = 17
		)

		switch len(cols) {
		case poolsColumnsPgBouncer1180:
			if err = rows.Scan(&database, &user,
				&clActive,
				&clWaiting,
				&clActiveCancelReq,
				&clWaitingCancelReq,
				&svActive,
				&svActiveCancel,
				&svBeingCanceled,
				&svIdle,
				&svUsed,
				&svTested,
				&svLogin,
				&maxWait,
				&maxWaitUs,
				&poolMode,
			); err != nil {
				contextLogger.Error(err, "Error while executing SHOW POOLS")
				e.Metrics.Error.Set(1)
				e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
			}
		case poolsColumnsPgBouncer1240:
			if err = rows.Scan(&database, &user,
				&clActive,
				&clWaiting,
				&clActiveCancelReq,
				&clWaitingCancelReq,
				&svActive,
				&svActiveCancel,
				&svBeingCanceled,
				&svIdle,
				&svUsed,
				&svTested,
				&svLogin,
				&maxWait,
				&maxWaitUs,
				&poolMode,
				&loadBalanceHosts,
			); err != nil {
				contextLogger.Error(err, "Error while executing SHOW POOLS")
				e.Metrics.Error.Set(1)
				e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
			}
		default:
			if err = rows.Scan(&database, &user,
				&clActive,
				&clWaiting,
				&clCancelReq,
				&svActive,
				&svIdle,
				&svUsed,
				&svTested,
				&svLogin,
				&maxWait,
				&maxWaitUs,
				&poolMode,
			); err != nil {
				contextLogger.Error(err, "Error while executing SHOW POOLS")
				e.Metrics.Error.Set(1)
				e.Metrics.PgCollectionErrors.WithLabelValues(err.Error()).Inc()
			}
		}
		e.Metrics.ShowPools.ClActive.WithLabelValues(database, user).Set(float64(clActive))
		e.Metrics.ShowPools.ClWaiting.WithLabelValues(database, user).Set(float64(clWaiting))
		e.Metrics.ShowPools.ClCancelReq.WithLabelValues(database, user).Set(float64(clCancelReq))
		e.Metrics.ShowPools.ClActiveCancelReq.WithLabelValues(database, user).Set(float64(clActiveCancelReq))
		e.Metrics.ShowPools.ClWaitingCancelReq.WithLabelValues(database, user).Set(float64(clWaitingCancelReq))
		e.Metrics.ShowPools.SvActive.WithLabelValues(database, user).Set(float64(svActive))
		e.Metrics.ShowPools.SvActiveCancel.WithLabelValues(database, user).Set(float64(svActiveCancel))
		e.Metrics.ShowPools.SvBeingCanceled.WithLabelValues(database, user).Set(float64(svBeingCanceled))
		e.Metrics.ShowPools.SvIdle.WithLabelValues(database, user).Set(float64(svIdle))
		e.Metrics.ShowPools.SvUsed.WithLabelValues(database, user).Set(float64(svUsed))
		e.Metrics.ShowPools.SvTested.WithLabelValues(database, user).Set(float64(svTested))
		e.Metrics.ShowPools.SvLogin.WithLabelValues(database, user).Set(float64(svLogin))
		e.Metrics.ShowPools.MaxWait.WithLabelValues(database, user).Set(float64(maxWait))
		e.Metrics.ShowPools.MaxWaitUs.WithLabelValues(database, user).Set(float64(maxWaitUs))
		e.Metrics.ShowPools.PoolMode.WithLabelValues(database, user).Set(float64(poolModeToInt(poolMode)))
		e.Metrics.ShowPools.LoadBalanceHosts.WithLabelValues(database, user).Set(float64(loadBalanceHosts.Int32))
	}

	e.Metrics.ShowPools.ClActive.Collect(ch)
	e.Metrics.ShowPools.ClWaiting.Collect(ch)
	e.Metrics.ShowPools.ClCancelReq.Collect(ch)
	e.Metrics.ShowPools.ClActiveCancelReq.Collect(ch)
	e.Metrics.ShowPools.ClWaitingCancelReq.Collect(ch)
	e.Metrics.ShowPools.SvActive.Collect(ch)
	e.Metrics.ShowPools.SvActiveCancel.Collect(ch)
	e.Metrics.ShowPools.SvBeingCanceled.Collect(ch)
	e.Metrics.ShowPools.SvIdle.Collect(ch)
	e.Metrics.ShowPools.SvUsed.Collect(ch)
	e.Metrics.ShowPools.SvTested.Collect(ch)
	e.Metrics.ShowPools.SvLogin.Collect(ch)
	e.Metrics.ShowPools.MaxWait.Collect(ch)
	e.Metrics.ShowPools.MaxWaitUs.Collect(ch)
	e.Metrics.ShowPools.PoolMode.Collect(ch)
	e.Metrics.ShowPools.LoadBalanceHosts.Collect(ch)

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
