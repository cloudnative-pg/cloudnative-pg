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

package metricserver

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/disk"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/wal"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// decisionMetricsCache caches auto-resize counts derived from cluster status to avoid O(N) scans on every scrape.
type decisionMetricsCache struct {
	mu              sync.Mutex
	resourceVersion string
	successCount    map[string]int // key: pvcName
	recentCount     map[string]int // key: pvcName
}

var dmCache = &decisionMetricsCache{
	successCount: make(map[string]int),
	recentCount:  make(map[string]int),
}

// DiskMetrics contains the Prometheus metric descriptors for disk usage.
type DiskMetrics struct {
	TotalBytes            *prometheus.GaugeVec
	UsedBytes             *prometheus.GaugeVec
	AvailableBytes        *prometheus.GaugeVec
	PercentUsed           *prometheus.GaugeVec
	InodesTotal           *prometheus.GaugeVec
	InodesUsed            *prometheus.GaugeVec
	InodesFree            *prometheus.GaugeVec
	AtLimit               *prometheus.GaugeVec
	ResizeBlocked         *prometheus.GaugeVec
	ResizesTotal          *prometheus.GaugeVec
	ResizeBudgetRemain    *prometheus.GaugeVec
	WALArchiveHealthy     prometheus.Gauge
	WALPendingFiles       prometheus.Gauge
	WALInactiveSlots      prometheus.Gauge
	WALSlotRetentionBytes *prometheus.GaugeVec
}

// newDiskMetrics returns the disk-related Prometheus metrics.
func newDiskMetrics() *DiskMetrics {
	return &DiskMetrics{
		TotalBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_total_bytes",
			Help:      "Total capacity of the volume in bytes",
		}, []string{"volume_type", "tablespace"}),
		UsedBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_used_bytes",
			Help:      "Used space on the volume in bytes",
		}, []string{"volume_type", "tablespace"}),
		AvailableBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_available_bytes",
			Help:      "Available space on the volume in bytes (non-root)",
		}, []string{"volume_type", "tablespace"}),
		PercentUsed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_percent_used",
			Help:      "Percentage of the volume in use (0-100)",
		}, []string{"volume_type", "tablespace"}),
		InodesTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_inodes_total",
			Help:      "Total number of inodes on the volume",
		}, []string{"volume_type", "tablespace"}),
		InodesUsed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_inodes_used",
			Help:      "Number of inodes in use on the volume",
		}, []string{"volume_type", "tablespace"}),
		InodesFree: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_inodes_free",
			Help:      "Number of free inodes on the volume",
		}, []string{"volume_type", "tablespace"}),
		AtLimit: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_at_limit",
			Help:      "1 if the volume is at its configured expansion.limit, 0 otherwise",
		}, []string{"volume_type", "tablespace"}),
		ResizeBlocked: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_resize_blocked",
			Help:      "1 if auto-resize is blocked, with reason label",
		}, []string{"volume_type", "tablespace", "reason"}),
		ResizesTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Subsystem: "disk",
			Name:      "resizes_total",
			Help:      "Total number of auto-resize operations",
		}, []string{"instance", "pvc_name", "volume_type", "tablespace", "result"}),
		ResizeBudgetRemain: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "disk_resize_budget_remaining",
			Help:      "Number of remaining auto-resize operations in the current 24h budget",
		}, []string{"instance", "pvc_name", "volume_type", "tablespace"}),
		WALArchiveHealthy: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "wal_archive_healthy",
			Help:      "1 if WAL archiving is healthy (last_archived_time > last_failed_time), 0 otherwise",
		}),
		WALPendingFiles: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "wal_pending_archive_files",
			Help:      "Number of WAL files pending archiving (.ready files in archive_status)",
		}),
		WALInactiveSlots: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "wal_inactive_slots",
			Help:      "Number of inactive physical replication slots",
		}),
		WALSlotRetentionBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: PrometheusNamespace,
			Name:      "wal_slot_retention_bytes",
			Help:      "WAL retention in bytes for inactive physical replication slots",
		}, []string{"slot_name"}),
	}
}

// describeDiskMetrics describes all disk-related metrics.
func (dm *DiskMetrics) describe(ch chan<- *prometheus.Desc) {
	dm.TotalBytes.Describe(ch)
	dm.UsedBytes.Describe(ch)
	dm.AvailableBytes.Describe(ch)
	dm.PercentUsed.Describe(ch)
	dm.InodesTotal.Describe(ch)
	dm.InodesUsed.Describe(ch)
	dm.InodesFree.Describe(ch)
	dm.AtLimit.Describe(ch)
	dm.ResizeBlocked.Describe(ch)
	dm.ResizesTotal.Describe(ch)
	dm.ResizeBudgetRemain.Describe(ch)
	ch <- dm.WALArchiveHealthy.Desc()
	ch <- dm.WALPendingFiles.Desc()
	ch <- dm.WALInactiveSlots.Desc()
	dm.WALSlotRetentionBytes.Describe(ch)
}

// collectDiskMetrics sends all disk-related metrics to the channel.
func (dm *DiskMetrics) collect(ch chan<- prometheus.Metric) {
	dm.TotalBytes.Collect(ch)
	dm.UsedBytes.Collect(ch)
	dm.AvailableBytes.Collect(ch)
	dm.PercentUsed.Collect(ch)
	dm.InodesTotal.Collect(ch)
	dm.InodesUsed.Collect(ch)
	dm.InodesFree.Collect(ch)
	dm.AtLimit.Collect(ch)
	dm.ResizeBlocked.Collect(ch)
	dm.ResizesTotal.Collect(ch)
	dm.ResizeBudgetRemain.Collect(ch)
	ch <- dm.WALArchiveHealthy
	ch <- dm.WALPendingFiles
	ch <- dm.WALInactiveSlots
	dm.WALSlotRetentionBytes.Collect(ch)
}

// setVolumeStats sets the disk usage metrics for a single volume.
func (dm *DiskMetrics) setVolumeStats(result *disk.VolumeProbeResult) {
	volType := string(result.VolumeType)
	ts := result.Tablespace

	dm.TotalBytes.WithLabelValues(volType, ts).Set(float64(result.Stats.TotalBytes))
	dm.UsedBytes.WithLabelValues(volType, ts).Set(float64(result.Stats.UsedBytes))
	dm.AvailableBytes.WithLabelValues(volType, ts).Set(float64(result.Stats.AvailableBytes))
	dm.PercentUsed.WithLabelValues(volType, ts).Set(result.Stats.PercentUsed)
	dm.InodesTotal.WithLabelValues(volType, ts).Set(float64(result.Stats.InodesTotal))
	dm.InodesUsed.WithLabelValues(volType, ts).Set(float64(result.Stats.InodesUsed))
	dm.InodesFree.WithLabelValues(volType, ts).Set(float64(result.Stats.InodesFree))
}

// resetVolumeStats clears the disk usage metrics for a volume to avoid stale data on probe failure.
func (dm *DiskMetrics) resetVolumeStats(volType string, ts string) {
	dm.TotalBytes.WithLabelValues(volType, ts).Set(0)
	dm.UsedBytes.WithLabelValues(volType, ts).Set(0)
	dm.AvailableBytes.WithLabelValues(volType, ts).Set(0)
	dm.PercentUsed.WithLabelValues(volType, ts).Set(0)
	dm.InodesTotal.WithLabelValues(volType, ts).Set(0)
	dm.InodesUsed.WithLabelValues(volType, ts).Set(0)
	dm.InodesFree.WithLabelValues(volType, ts).Set(0)
}

// collectWALHealthMetrics queries WAL archive health and updates metrics.
func collectWALHealthMetrics(ctx context.Context, e *Exporter, db *sql.DB, isPrimary bool) {
	contextLogger := log.FromContext(ctx).WithName("wal_health_metrics")

	getReadyWALCount := func() (int, error) {
		ready, _, err := postgres.GetWALArchiveCounters()
		return ready, err
	}

	checker := wal.NewHealthChecker(getReadyWALCount)
	status, err := checker.Check(ctx, db, isPrimary)
	if err != nil {
		contextLogger.Error(err, "failed to check WAL health")
		// Reset gauges to avoid reporting stale data on failure
		e.Metrics.DiskMetrics.WALArchiveHealthy.Set(0)
		e.Metrics.DiskMetrics.WALPendingFiles.Set(0)
		e.Metrics.DiskMetrics.WALInactiveSlots.Set(0)
		e.Metrics.DiskMetrics.WALSlotRetentionBytes.Reset()
		return
	}

	if status.ArchiveHealthy {
		e.Metrics.DiskMetrics.WALArchiveHealthy.Set(1)
	} else {
		e.Metrics.DiskMetrics.WALArchiveHealthy.Set(0)
	}

	e.Metrics.DiskMetrics.WALPendingFiles.Set(float64(status.PendingWALFiles))
	e.Metrics.DiskMetrics.WALInactiveSlots.Set(float64(len(status.InactiveSlots)))

	// Reset slot retention metrics before re-populating
	e.Metrics.DiskMetrics.WALSlotRetentionBytes.Reset()
	for _, slot := range status.InactiveSlots {
		e.Metrics.DiskMetrics.WALSlotRetentionBytes.WithLabelValues(slot.SlotName).Set(float64(slot.RetentionBytes))
	}
}

// collectDiskUsageMetrics probes all volumes (PGDATA, WAL, tablespaces) and updates metrics.
func collectDiskUsageMetrics(e *Exporter) {
	contextLogger := log.WithName("disk_metrics")
	probe := disk.NewProbe()

	cluster, clusterErr := e.getCluster()
	localPodName := e.instance.GetPodName()

	// Probe PGDATA volume
	dataResult, err := probe.ProbeVolume(specs.PgDataPath, disk.VolumeTypeData, "")
	if err != nil {
		contextLogger.Error(err, "failed to probe PGDATA volume")
		e.Metrics.DiskMetrics.resetVolumeStats(string(disk.VolumeTypeData), "")
	} else {
		e.Metrics.DiskMetrics.setVolumeStats(dataResult)
		if clusterErr == nil {
			e.Metrics.DiskMetrics.deriveDecisionMetrics(cluster, dataResult, localPodName)
		}
	}

	// Probe WAL volume if it exists (separate from PGDATA)
	if clusterErr == nil && cluster.ShouldCreateWalArchiveVolume() {
		walResult, err := probe.ProbeVolume(specs.PgWalVolumePath, disk.VolumeTypeWAL, "")
		if err != nil {
			contextLogger.Error(err, "failed to probe WAL volume")
			e.Metrics.DiskMetrics.resetVolumeStats(string(disk.VolumeTypeWAL), "")
		} else {
			e.Metrics.DiskMetrics.setVolumeStats(walResult)
			e.Metrics.DiskMetrics.deriveDecisionMetrics(cluster, walResult, localPodName)
		}
	}

	// Probe tablespace volumes
	if clusterErr == nil {
		for _, tbsConfig := range cluster.Spec.Tablespaces {
			tbsPath := specs.MountForTablespace(tbsConfig.Name)
			tbsResult, err := probe.ProbeVolume(tbsPath, disk.VolumeTypeTablespace, tbsConfig.Name)
			if err != nil {
				contextLogger.Error(err, "failed to probe tablespace volume",
					"tablespace", tbsConfig.Name)
				e.Metrics.DiskMetrics.resetVolumeStats(string(disk.VolumeTypeTablespace), tbsConfig.Name)
			} else {
				e.Metrics.DiskMetrics.setVolumeStats(tbsResult)
				e.Metrics.DiskMetrics.deriveDecisionMetrics(cluster, tbsResult, localPodName)
			}
		}
	}
}

// deriveDecisionMetrics populates logical metrics (totals, budget) from cluster status history.
// The localPodName parameter is the name of the pod running this collector, ensuring each
// pod emits metrics with its own labels rather than the primary's.
func (dm *DiskMetrics) deriveDecisionMetrics(
	cluster *apiv1.Cluster,
	probe *disk.VolumeProbeResult,
	localPodName string,
) {
	var pvcName string
	switch probe.VolumeType {
	case disk.VolumeTypeData:
		pvcName = localPodName
	case disk.VolumeTypeWAL:
		pvcName = localPodName + apiv1.WalArchiveVolumeSuffix
	case disk.VolumeTypeTablespace:
		pvcName = specs.PvcNameForTablespace(localPodName, probe.Tablespace)
	}

	volType := string(probe.VolumeType)
	ts := probe.Tablespace

	dmCache.mu.Lock()
	if dmCache.resourceVersion != cluster.ResourceVersion {
		// Version changed, refresh cache
		newSuccess := make(map[string]int)
		newRecent := make(map[string]int)
		cutoff := time.Now().Add(-24 * time.Hour)

		for _, event := range cluster.Status.AutoResizeEvents {
			if event.Result == apiv1.ResizeResultSuccess {
				newSuccess[event.PVCName]++
				if event.Timestamp.After(cutoff) {
					newRecent[event.PVCName]++
				}
			}
		}
		dmCache.successCount = newSuccess
		dmCache.recentCount = newRecent
		dmCache.resourceVersion = cluster.ResourceVersion
	}
	successCount := dmCache.successCount[pvcName]
	recentCount := dmCache.recentCount[pvcName]
	dmCache.mu.Unlock()

	dm.ResizesTotal.WithLabelValues(localPodName, pvcName, volType, ts, "success").Set(float64(successCount))

	// 2. Calculate remaining budget (24h window)
	maxActions := 3 // default
	resizeConfig := getResizeConfig(cluster, probe.VolumeType, ts)
	if resizeConfig != nil && resizeConfig.Strategy != nil && resizeConfig.Strategy.MaxActionsPerDay != nil {
		maxActions = *resizeConfig.Strategy.MaxActionsPerDay
	}

	budgetRemain := maxActions - recentCount
	if budgetRemain < 0 {
		budgetRemain = 0
	}
	dm.ResizeBudgetRemain.WithLabelValues(localPodName, pvcName, volType, ts).Set(float64(budgetRemain))

	// 3. At Limit metric and blocked status
	atLimit := false
	if resizeConfig != nil && resizeConfig.Expansion != nil && resizeConfig.Expansion.Limit != "" {
		limit, err := resource.ParseQuantity(resizeConfig.Expansion.Limit)
		//nolint:gosec // G115 - limit sizes cannot be negative in practice
		if err == nil && uint64(limit.Value()) <= probe.Stats.TotalBytes {
			dm.AtLimit.WithLabelValues(volType, ts).Set(1)
			atLimit = true
		} else {
			dm.AtLimit.WithLabelValues(volType, ts).Set(0)
		}
	}

	// 4. Resize blocked metric - set when auto-resize cannot proceed
	// Only populate if auto-resize is enabled for this volume
	if resizeConfig != nil && ptr.Deref(resizeConfig.Enabled, false) {
		if budgetRemain == 0 {
			dm.ResizeBlocked.WithLabelValues(volType, ts, "rate_limit").Set(1)
		} else {
			dm.ResizeBlocked.WithLabelValues(volType, ts, "rate_limit").Set(0)
		}

		if atLimit {
			dm.ResizeBlocked.WithLabelValues(volType, ts, "expansion_limit").Set(1)
		} else {
			dm.ResizeBlocked.WithLabelValues(volType, ts, "expansion_limit").Set(0)
		}
	}
}

func getResizeConfig(cluster *apiv1.Cluster, volType disk.VolumeType, tsName string) *apiv1.ResizeConfiguration {
	switch volType {
	case disk.VolumeTypeData:
		return cluster.Spec.StorageConfiguration.Resize
	case disk.VolumeTypeWAL:
		if cluster.Spec.WalStorage != nil {
			return cluster.Spec.WalStorage.Resize
		}
	case disk.VolumeTypeTablespace:
		for _, tbs := range cluster.Spec.Tablespaces {
			if tbs.Name == tsName {
				return tbs.Storage.Resize
			}
		}
	}
	return nil
}
