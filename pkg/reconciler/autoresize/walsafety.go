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

package autoresize

import (
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// WALSafetyBlockReason represents the reason a WAL safety check blocked a resize.
type WALSafetyBlockReason string

const (
	// WALSafetyBlockArchiveUnhealthy indicates the resize was blocked because WAL archiving is unhealthy.
	WALSafetyBlockArchiveUnhealthy WALSafetyBlockReason = "archive_unhealthy"

	// WALSafetyBlockPendingWAL indicates the resize was blocked because pending WAL files exceeded the threshold.
	WALSafetyBlockPendingWAL WALSafetyBlockReason = "pending_wal_exceeded"

	// WALSafetyBlockSlotRetention indicates the resize was blocked because slot WAL retention exceeded the threshold.
	WALSafetyBlockSlotRetention WALSafetyBlockReason = "slot_retention_exceeded"

	// WALSafetyBlockSingleVolumeNoAck indicates the resize was blocked because a single-volume cluster
	// did not have acknowledgeWALRisk set.
	WALSafetyBlockSingleVolumeNoAck WALSafetyBlockReason = "single_volume_no_ack"

	// WALSafetyBlockHealthUnavailable indicates WAL health data was unavailable.
	WALSafetyBlockHealthUnavailable WALSafetyBlockReason = "wal_health_unavailable"

	// DefaultMaxPendingWALFiles is the default maximum number of pending WAL files before blocking resize.
	DefaultMaxPendingWALFiles = 100
)

// WALSafetyResult contains the result of a WAL safety evaluation.
type WALSafetyResult struct {
	// Allowed is true if the resize is allowed to proceed.
	Allowed bool

	// BlockReason indicates the reason for blocking or a warning condition.
	// Set to a specific reason when Allowed is false (hard block).
	// May be set to WALSafetyBlockHealthUnavailable when Allowed is true (warning).
	BlockReason WALSafetyBlockReason

	// BlockMessage is a human-readable description of why the resize was blocked.
	BlockMessage string
}

// EvaluateWALSafety evaluates WAL safety checks for a PVC resize operation.
// It checks whether:
//   - Single-volume clusters have acknowledged WAL risk
//   - WAL archiving is healthy (if requireArchiveHealthy)
//   - Pending WAL files are below threshold (if maxPendingWALFiles configured)
//   - Slot WAL retention is below threshold (if maxSlotRetentionBytes configured)
//
// Parameters:
//   - pvcRole: the role of the PVC being resized (data/wal/tablespace)
//   - isSingleVolume: true if the cluster has no separate WAL storage
//   - walSafety: the WAL safety policy from the resize configuration (may be nil)
//   - walHealth: the current WAL health status (may be nil)
func EvaluateWALSafety(
	pvcRole string,
	isSingleVolume bool,
	walSafety *apiv1.WALSafetyPolicy,
	walHealth *postgres.WALHealthStatus,
) WALSafetyResult {
	// WAL safety only applies to data volumes (in single-volume clusters) and WAL volumes.
	// Tablespace volumes are not affected by WAL concerns.
	if !needsWALSafetyCheck(pvcRole, isSingleVolume) {
		return WALSafetyResult{Allowed: true}
	}

	// Single-volume clusters must explicitly acknowledge WAL risk
	if isSingleVolume && pvcRole == string(utils.PVCRolePgData) {
		if walSafety == nil || walSafety.AcknowledgeWALRisk == nil || !*walSafety.AcknowledgeWALRisk {
			return WALSafetyResult{
				Allowed:      false,
				BlockReason:  WALSafetyBlockSingleVolumeNoAck,
				BlockMessage: "auto-resize blocked: single-volume cluster requires acknowledgeWALRisk=true in walSafetyPolicy",
			}
		}
	}

	// If no WAL health data is available, allow the resize (fail-open) with a warning.
	// The primary threat is disk full → database crash. Blocking resize when we can't
	// verify WAL health is more dangerous than allowing it.
	if walHealth == nil {
		return WALSafetyResult{
			Allowed:      true,
			BlockReason:  WALSafetyBlockHealthUnavailable,
			BlockMessage: "auto-resize permitted without WAL health verification: WAL health information is not available",
		}
	}

	// If no WAL safety policy is configured, use defaults
	if walSafety == nil {
		walSafety = defaultWALSafetyPolicy()
	}

	// Check archive health
	requireArchiveHealthy := true
	if walSafety.RequireArchiveHealthy != nil {
		requireArchiveHealthy = *walSafety.RequireArchiveHealthy
	}
	if requireArchiveHealthy && !walHealth.ArchiveHealthy {
		return WALSafetyResult{
			Allowed:      false,
			BlockReason:  WALSafetyBlockArchiveUnhealthy,
			BlockMessage: "auto-resize blocked: WAL archive is unhealthy (last_failed_time > last_archived_time)",
		}
	}

	// Check pending WAL files
	maxPendingWAL := DefaultMaxPendingWALFiles
	if walSafety.MaxPendingWALFiles != nil {
		maxPendingWAL = *walSafety.MaxPendingWALFiles
	}
	if maxPendingWAL > 0 && walHealth.PendingWALFiles > maxPendingWAL {
		return WALSafetyResult{
			Allowed:     false,
			BlockReason: WALSafetyBlockPendingWAL,
			BlockMessage: fmt.Sprintf(
				"auto-resize blocked: pending WAL files (%d) exceeds threshold (%d)",
				walHealth.PendingWALFiles, maxPendingWAL),
		}
	}

	// Check inactive slot WAL retention
	if walSafety.MaxSlotRetentionBytes != nil && *walSafety.MaxSlotRetentionBytes > 0 {
		maxRetention := *walSafety.MaxSlotRetentionBytes
		for _, slot := range walHealth.InactiveSlots {
			if slot.RetentionBytes > maxRetention {
				return WALSafetyResult{
					Allowed:     false,
					BlockReason: WALSafetyBlockSlotRetention,
					BlockMessage: fmt.Sprintf(
						"auto-resize blocked: inactive slot %q retains %d bytes (threshold: %d bytes)",
						slot.SlotName, slot.RetentionBytes, maxRetention),
				}
			}
		}
	}

	return WALSafetyResult{Allowed: true}
}

// needsWALSafetyCheck determines if WAL safety checks are needed for this PVC role.
func needsWALSafetyCheck(pvcRole string, isSingleVolume bool) bool {
	switch pvcRole {
	case string(utils.PVCRolePgWal):
		// WAL volumes always need WAL safety checks
		return true
	case string(utils.PVCRolePgData):
		// Data volumes need WAL safety checks only in single-volume clusters
		// (where WAL shares the data volume)
		return isSingleVolume
	default:
		// Tablespace and other volumes don't need WAL safety checks
		return false
	}
}

// defaultWALSafetyPolicy returns a WAL safety policy with default values.
func defaultWALSafetyPolicy() *apiv1.WALSafetyPolicy {
	requireArchiveHealthy := true
	maxPendingWALFiles := DefaultMaxPendingWALFiles
	return &apiv1.WALSafetyPolicy{
		RequireArchiveHealthy: &requireArchiveHealthy,
		MaxPendingWALFiles:    &maxPendingWALFiles,
	}
}
