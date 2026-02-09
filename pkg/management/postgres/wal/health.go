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

// Package wal provides WAL archive health checking functionality.
package wal

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// HealthStatus contains the health status of WAL archiving and replication slots.
type HealthStatus struct {
	// ArchiveHealthy indicates whether the WAL archive process is healthy.
	// True when last_archived_time > last_failed_time or no failures have occurred.
	ArchiveHealthy bool `json:"archiveHealthy"`
	// LastArchivedTime is the timestamp of the last successful archive operation.
	LastArchivedTime *time.Time `json:"lastArchivedTime,omitempty"`
	// LastFailedTime is the timestamp of the last failed archive operation.
	LastFailedTime *time.Time `json:"lastFailedTime,omitempty"`
	// ArchiverFailedCount is the total number of failed archive operations since last stats reset.
	ArchiverFailedCount int64 `json:"archiverFailedCount"`
	// PendingWALFiles is the count of .ready files in pg_wal/archive_status/.
	PendingWALFiles int `json:"pendingWALFiles"`
	// InactiveSlots contains information about inactive physical replication slots.
	InactiveSlots []InactiveSlotInfo `json:"inactiveSlots,omitempty"`
}

// InactiveSlotInfo contains information about an inactive physical replication slot
// and its WAL retention.
type InactiveSlotInfo struct {
	// SlotName is the name of the replication slot.
	SlotName string `json:"slotName"`
	// RetentionBytes is the WAL retention in bytes caused by this slot.
	// Calculated using pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn).
	RetentionBytes int64 `json:"retentionBytes"`
}

// HealthChecker checks WAL archive health by querying PostgreSQL system views
// and checking the WAL archive status directory.
type HealthChecker struct {
	// getReadyWALCount is the function to count .ready WAL files.
	// This can be replaced for testing.
	getReadyWALCount ReadyWALCountFunc
}

// ReadyWALCountFunc is the function signature for counting ready WAL files.
type ReadyWALCountFunc func() (int, error)

// DBQuerier is the interface for executing database queries.
// This abstraction allows mocking in tests.
type DBQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// NewHealthChecker creates a new HealthChecker with the given function
// for counting ready WAL files.
func NewHealthChecker(getReadyWALCount ReadyWALCountFunc) *HealthChecker {
	return &HealthChecker{
		getReadyWALCount: getReadyWALCount,
	}
}

// Check performs a comprehensive WAL health check by:
// 1. Counting .ready files in pg_wal/archive_status/
// 2. Querying pg_stat_archiver for archive status
// 3. Querying pg_replication_slots for inactive physical slots with WAL retention
func (h *HealthChecker) Check(ctx context.Context, db DBQuerier, isPrimary bool) (*HealthStatus, error) {
	contextLogger := log.FromContext(ctx).WithName("wal_health")

	status := &HealthStatus{
		ArchiveHealthy: true,
	}
	encounteredError := false

	// Count .ready WAL files
	readyCount, err := h.getReadyWALCount()
	if err != nil {
		contextLogger.Error(err, "failed to count ready WAL files")
		encounteredError = true
	} else {
		status.PendingWALFiles = readyCount
	}

	// Query pg_stat_archiver
	if err := h.queryArchiverStatus(db, status); err != nil {
		contextLogger.Error(err, "failed to query archiver status")
		encounteredError = true
	}

	// Query inactive replication slots (only on primary)
	if isPrimary {
		if err := h.queryInactiveSlots(db, status); err != nil {
			contextLogger.Error(err, "failed to query inactive replication slots")
			encounteredError = true
		}
		contextLogger.Info("queried inactive replication slots",
			"isPrimary", isPrimary,
			"inactiveSlots", len(status.InactiveSlots))
	} else {
		contextLogger.Info("skipping inactive slot query (not primary)",
			"isPrimary", isPrimary)
	}

	if encounteredError {
		return nil, fmt.Errorf("wal health check incomplete")
	}

	return status, nil
}

// queryArchiverStatus queries pg_stat_archiver for archive health information.
func (h *HealthChecker) queryArchiverStatus(db DBQuerier, status *HealthStatus) error {
	var (
		lastArchivedTime sql.NullTime
		lastFailedTime   sql.NullTime
		failedCount      int64
	)

	err := db.QueryRow(`
		SELECT
			last_archived_time,
			last_failed_time,
			failed_count
		FROM pg_catalog.pg_stat_archiver
	`).Scan(&lastArchivedTime, &lastFailedTime, &failedCount)
	if err != nil {
		return err
	}

	status.ArchiverFailedCount = failedCount

	if lastArchivedTime.Valid {
		status.LastArchivedTime = &lastArchivedTime.Time
	}
	if lastFailedTime.Valid {
		status.LastFailedTime = &lastFailedTime.Time
	}

	// Archive is healthy if:
	// - No failures have ever occurred, OR
	// - The last successful archive is more recent than the last failure
	if lastFailedTime.Valid {
		if !lastArchivedTime.Valid || lastFailedTime.Time.After(lastArchivedTime.Time) {
			status.ArchiveHealthy = false
		}
	}

	return nil
}

// queryInactiveSlots queries pg_replication_slots for inactive physical slots
// and calculates their WAL retention using pg_wal_lsn_diff.
func (h *HealthChecker) queryInactiveSlots(db DBQuerier, status *HealthStatus) error {
	rows, err := db.Query(`
		SELECT
			slot_name,
			COALESCE(pg_catalog.pg_wal_lsn_diff(pg_catalog.pg_current_wal_lsn(), restart_lsn), 0)::bigint AS retention_bytes
		FROM pg_catalog.pg_replication_slots
		WHERE NOT active
			AND slot_type = 'physical'
			AND restart_lsn IS NOT NULL
	`)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Error(closeErr, "while closing rows for inactive slots")
		}
	}()

	for rows.Next() {
		var slot InactiveSlotInfo
		if err := rows.Scan(&slot.SlotName, &slot.RetentionBytes); err != nil {
			return err
		}
		status.InactiveSlots = append(status.InactiveSlots, slot)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("while iterating over inactive replication slots: %w", err)
	}

	return nil
}
