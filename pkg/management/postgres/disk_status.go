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

package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/disk"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/wal"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// fillDiskStatus probes the filesystem for PGDATA, WAL, and tablespace volumes
// and populates the disk status in the result.
func (instance *Instance) fillDiskStatus(result *postgresSpec.PostgresqlStatus) {
	contextLogger := log.WithName("disk_status")

	probe := disk.NewProbe()
	diskStatus := &postgresSpec.DiskStatus{}

	// Probe PGDATA volume
	dataStats, err := probe.GetVolumeStats(specs.PgDataPath)
	if err != nil {
		contextLogger.Error(err, "failed to probe PGDATA volume for status")
	} else {
		diskStatus.DataVolume = volumeStatusFromStats(dataStats)
	}

	// Probe WAL volume if separate
	cluster := instance.Cluster
	if cluster != nil && cluster.ShouldCreateWalArchiveVolume() {
		walStats, err := probe.GetVolumeStats(specs.PgWalVolumePath)
		if err != nil {
			contextLogger.Error(err, "failed to probe WAL volume for status")
		} else {
			diskStatus.WALVolume = volumeStatusFromStats(walStats)
		}
	}

	// Probe tablespace volumes
	if cluster != nil {
		for _, tbsConfig := range cluster.Spec.Tablespaces {
			tbsPath := specs.MountForTablespace(tbsConfig.Name)
			tbsStats, err := probe.GetVolumeStats(tbsPath)
			if err != nil {
				contextLogger.Error(err, "failed to probe tablespace volume for status",
					"tablespace", tbsConfig.Name)
				continue
			}
			if diskStatus.Tablespaces == nil {
				diskStatus.Tablespaces = make(map[string]*postgresSpec.VolumeStatus)
			}
			diskStatus.Tablespaces[tbsConfig.Name] = volumeStatusFromStats(tbsStats)
		}
	}

	result.DiskStatus = diskStatus
}

// volumeStatusFromStats converts disk.VolumeStats to postgres.VolumeStatus.
func volumeStatusFromStats(stats *disk.VolumeStats) *postgresSpec.VolumeStatus {
	return &postgresSpec.VolumeStatus{
		TotalBytes:     stats.TotalBytes,
		UsedBytes:      stats.UsedBytes,
		AvailableBytes: stats.AvailableBytes,
		PercentUsed:    stats.PercentUsed,
		InodesTotal:    stats.InodesTotal,
		InodesUsed:     stats.InodesUsed,
		InodesFree:     stats.InodesFree,
	}
}

// fillWALHealthStatus checks WAL archive health and populates the WAL health status in the result.
func (instance *Instance) fillWALHealthStatus(ctx context.Context, db *sql.DB, result *postgresSpec.PostgresqlStatus) {
	contextLogger := log.FromContext(ctx).WithName("wal_health_status")

	archiveStatusPath := filepath.Join(specs.PgWalPath, "archive_status")
	getReadyWALCount := func() (int, error) {
		return countReadyWALFiles(archiveStatusPath)
	}

	checker := wal.NewHealthChecker(getReadyWALCount)
	contextLogger.Info("checking WAL health",
		"isPrimary", result.IsPrimary)
	healthStatus, err := checker.Check(ctx, db, result.IsPrimary)
	if err != nil {
		contextLogger.Error(err, "failed to check WAL health for status")
		return
	}
	contextLogger.Info("WAL health check completed",
		"archiveHealthy", healthStatus.ArchiveHealthy,
		"inactiveSlotCount", len(healthStatus.InactiveSlots))

	walHealthStatus := &postgresSpec.WALHealthStatus{
		ArchiveHealthy:      healthStatus.ArchiveHealthy,
		PendingWALFiles:     healthStatus.PendingWALFiles,
		ArchiverFailedCount: healthStatus.ArchiverFailedCount,
		InactiveSlotCount:   len(healthStatus.InactiveSlots),
	}

	for _, slot := range healthStatus.InactiveSlots {
		walHealthStatus.InactiveSlots = append(walHealthStatus.InactiveSlots, postgresSpec.WALInactiveSlotInfo{
			SlotName:       slot.SlotName,
			RetentionBytes: slot.RetentionBytes,
		})
	}

	result.WALHealthStatus = walHealthStatus
}

// countReadyWALFiles counts .ready files in the WAL archive_status directory.
func countReadyWALFiles(archiveStatusPath string) (int, error) {
	entries, err := os.ReadDir(archiveStatusPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".ready" {
			count++
		}
	}
	return count, nil
}
