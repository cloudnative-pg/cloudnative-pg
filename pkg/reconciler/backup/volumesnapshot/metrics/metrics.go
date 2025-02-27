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

// Package metrics contains metrics for the volume snapshot reconciler
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// PhaseType represents the phase of the volume snapshot operation
type PhaseType string

// Volume snapshot phases for metrics
const (
	PhaseProvisioning PhaseType = "provisioning"
	PhaseReady        PhaseType = "ready"
)

var (
	// VolumeSnapshotRetryTotal is a counter for the total number of retry operations
	VolumeSnapshotRetryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cnpg_volumesnapshot_retry_operations_total",
			Help: "Total number of retry operations for volume snapshots",
		},
		[]string{"namespace", "snapshot_name", "phase"},
	)

	// VolumeSnapshotRetryByErrorTotal is a counter for retries by error type
	VolumeSnapshotRetryByErrorTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cnpg_volumesnapshot_retry_by_error_type_total",
			Help: "Total number of retry operations by error type",
		},
		[]string{"namespace", "snapshot_name", "error_type", "phase"},
	)

	// VolumeSnapshotFailedTotal is a gauge for failed operations after retries
	VolumeSnapshotFailedTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cnpg_volumesnapshot_failed_operations_total",
			Help: "Total number of failed volume snapshot operations after maximum retries",
		},
		[]string{"namespace", "snapshot_name", "phase"},
	)

	// VolumeSnapshotRetryDuration is a histogram for retry duration
	VolumeSnapshotRetryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "cnpg_volumesnapshot_retry_duration_seconds",
			Help: "Duration of volume snapshot retry operations in seconds",
			Buckets: []float64{
				1, 5, 10, 30, 60, 120, 300, 600,
			},
		},
		[]string{"namespace", "snapshot_name", "phase"},
	)
)

func init() {
	// Register metrics with the controller runtime metrics registry
	metrics.Registry.MustRegister(
		VolumeSnapshotRetryTotal,
		VolumeSnapshotRetryByErrorTotal,
		VolumeSnapshotFailedTotal,
		VolumeSnapshotRetryDuration,
	)
}

// RecordRetry increments the retry counter for a volume snapshot operation
func RecordRetry(namespace, name string, phase PhaseType) {
	VolumeSnapshotRetryTotal.WithLabelValues(namespace, name, string(phase)).Inc()
}

// RecordRetryWithError increments the retry counter for a specific error type
func RecordRetryWithError(namespace, name string, phase PhaseType, errorType string) {
	VolumeSnapshotRetryByErrorTotal.WithLabelValues(
		namespace, name, errorType, string(phase)).Inc()
}

// RecordFailed increments the failed operations counter
func RecordFailed(namespace, name string, phase PhaseType) {
	VolumeSnapshotFailedTotal.WithLabelValues(namespace, name, string(phase)).Inc()
}

// RecordRetryDuration records the duration of a retry operation
func RecordRetryDuration(namespace, name string, phase PhaseType, durationSeconds float64) {
	VolumeSnapshotRetryDuration.WithLabelValues(namespace, name, string(phase)).Observe(durationSeconds)
}
