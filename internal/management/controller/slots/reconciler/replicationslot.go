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

package reconciler

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"
)

// ReconcileReplicationSlots reconciles the replication slots of a given instance
func ReconcileReplicationSlots(
	ctx context.Context,
	instanceName string,
	db *sql.DB,
	cluster *apiv1.Cluster,
) (reconcile.Result, error) {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil {
		return reconcile.Result{}, nil
	}

	isPrimary := cluster.Status.CurrentPrimary == instanceName || cluster.Status.TargetPrimary == instanceName

	// If the HA replication slots feature is turned off, we will remove all the HA
	// replication slots on both the primary and standby servers.
	// NOTE: If both the HA replication slots and the user defined replication slots features are disabled,
	// we also clean up the slots that fall under the user defined replication slots feature here.
	// TODO: split-out user defined replication slots code
	if !cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled() {
		return dropReplicationSlots(ctx, db, cluster, isPrimary)
	}

	// Clean up orphaned logical slots on replicas when synchronizeLogicalDecoding is enabled.
	// After switchover, a former primary retains its locally-created logical slots (synced=false).
	// PostgreSQL's slot sync worker cannot overwrite these slots because slots with synced=false
	// are considered locally-owned and read-only to the sync process. We must drop them so
	// the sync worker can recreate them with synced=true.
	if !isPrimary && cluster.Spec.ReplicationSlots.HighAvailability.GetSynchronizeLogicalDecoding() {
		contextLogger := log.FromContext(ctx)
		pgMajor, err := cluster.GetPostgresqlMajorVersion()
		if err != nil {
			// Log with full context to help debugging. Orphaned slots won't be cleaned up
			// until this is resolved, but we continue reconciliation to avoid blocking
			// other slot management operations.
			contextLogger.Warning("Unable to retrieve PostgreSQL major version for logical slot cleanup; "+
				"orphaned slots will not be cleaned up until this is resolved",
				"clusterName", cluster.Name,
				"imageName", cluster.Spec.ImageName,
				"err", err)
		} else if pgMajor >= 17 {
			if err := cleanupOrphanedLogicalSlots(ctx, db); err != nil {
				return reconcile.Result{}, fmt.Errorf("cleaning up orphaned logical slots: %w", err)
			}
		}
	}

	if isPrimary {
		return reconcilePrimaryHAReplicationSlots(ctx, db, cluster)
	}

	return reconcile.Result{}, nil
}

// reconcilePrimaryHAReplicationSlots reconciles the HA replication slots of the primary instance
func reconcilePrimaryHAReplicationSlots(
	ctx context.Context,
	db *sql.DB,
	cluster *apiv1.Cluster,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Updating primary HA replication slots")

	currentSlots, err := infrastructure.List(ctx, db, cluster.Spec.ReplicationSlots)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("reconciling primary replication slots: %w", err)
	}
	// expectedSlots is the set of the expected HA replication slot names
	expectedSlots := make(map[string]bool)

	// Add every slot that is missing
	for _, instanceName := range cluster.Status.InstanceNames {
		if instanceName == cluster.Status.CurrentPrimary {
			continue
		}

		slotName := cluster.GetSlotNameFromInstanceName(instanceName)
		expectedSlots[slotName] = true

		if currentSlots.Has(slotName) {
			continue
		}

		// At this point, the cluster instance does not have a HA replication slot
		if err := infrastructure.Create(ctx, db, infrastructure.ReplicationSlot{SlotName: slotName}); err != nil {
			return reconcile.Result{}, fmt.Errorf("creating primary HA replication slots: %w", err)
		}
	}

	contextLogger.Trace("Status of primary HA replication slots",
		"currentSlots", currentSlots,
		"expectedSlots", expectedSlots)

	// Delete any HA replication slots in the instance that is not from an existing cluster instance
	needToReschedule := false
	for _, slot := range currentSlots.Items {
		if !slot.IsHA {
			contextLogger.Trace("Skipping non-HA replication slot", "slot", slot)
			continue
		}
		if !expectedSlots[slot.SlotName] {
			// Avoid deleting active slots.
			// It would trow an error on Postgres side.
			if slot.Active {
				contextLogger.Trace("Skipping deletion of replication slot because it is active",
					"slot", slot)
				needToReschedule = true
				continue
			}
			contextLogger.Trace("Attempt to delete replication slot",
				"slot", slot)
			if err := infrastructure.Delete(ctx, db, slot); err != nil {
				return reconcile.Result{}, fmt.Errorf("failure deleting replication slot %q: %w", slot.SlotName, err)
			}
		}
	}

	if needToReschedule {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	return reconcile.Result{}, nil
}

// dropReplicationSlots cleans up the HA replication slots when the feature is disabled.
// If both the HA replication slots and the user defined replication slots features are disabled,
// we also clean up the slots that fall under the user defined replication slots feature here.
func dropReplicationSlots(
	ctx context.Context,
	db *sql.DB,
	cluster *apiv1.Cluster,
	isPrimary bool,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)

	// If, at the same time, the HA replication slots and the user defined replication slots features are disabled,
	// we must clean up all the replication slots on the standbys.
	dropUserSlots := !cluster.Spec.ReplicationSlots.SynchronizeReplicas.GetEnabled()

	// we fetch all replication slots
	slots, err := infrastructure.List(ctx, db, cluster.Spec.ReplicationSlots)
	if err != nil {
		return reconcile.Result{}, err
	}

	needToReschedule := false
	for _, slot := range slots.Items {
		// On the primary,  we only drop the HA replication slots
		if !slot.IsHA && isPrimary {
			continue
		}

		// Non-HA slots are only considered if dropUserSlots is true
		if !slot.IsHA && !dropUserSlots {
			continue
		}

		if slot.Active {
			contextLogger.Trace("Skipping deletion of replication slot because it is active",
				"slot", slot)
			needToReschedule = true
			continue
		}
		contextLogger.Trace("Attempt to delete replication slot",
			"slot", slot)
		if err := infrastructure.Delete(ctx, db, slot); err != nil {
			return reconcile.Result{}, fmt.Errorf("while disabling standby HA replication slots: %w", err)
		}
	}

	if needToReschedule {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	return reconcile.Result{}, nil
}

// cleanupOrphanedLogicalSlots removes orphaned failover-enabled logical slots.
// This function should only be called on PostgreSQL 17+ replicas.
//
// An orphaned failover slot is one where:
// - synced=false: It was created locally (when this instance was primary)
// - failover=true: It was configured for failover synchronization
// - active=false: No consumer is connected (required to drop)
//
// After a switchover, the demoted primary retains its locally-created failover slots.
// PostgreSQL's slot sync worker cannot overwrite these because synced=false slots
// are read-only to the sync process. We drop them so the sync worker can recreate
// them with synced=true.
//
// We specifically require failover=true to avoid dropping legitimate external
// subscription slots (like those created by pg_createsubscription), which have
// failover=false and should not be touched.
func cleanupOrphanedLogicalSlots(ctx context.Context, db *sql.DB) error {
	contextLog := log.FromContext(ctx).WithName("cleanupOrphanedLogicalSlots")

	slots, err := infrastructure.ListLogicalSlotsWithSyncStatus(ctx, db)
	if err != nil {
		return fmt.Errorf("listing logical slots: %w", err)
	}

	for _, slot := range slots {
		// Skip synced slots - they are properly managed by the sync worker
		if slot.Synced {
			contextLog.Trace("Skipping synced logical slot", "slotName", slot.SlotName)
			continue
		}

		// Skip non-failover slots - these are external subscription slots that should not be touched
		if !slot.Failover {
			contextLog.Trace("Skipping non-failover logical slot (likely external subscription)",
				"slotName", slot.SlotName)
			continue
		}

		// Skip active slots - they cannot be dropped and we'll retry on next reconciliation
		if slot.Active {
			contextLog.Trace("Skipping active orphaned logical slot (cannot be dropped)",
				"slotName", slot.SlotName)
			continue
		}

		// Drop orphaned failover slot (synced=false AND failover=true AND active=false)
		contextLog.Info("Dropping orphaned logical slot", "slotName", slot.SlotName)

		if err := infrastructure.DeleteLogicalSlot(ctx, db, slot.SlotName); err != nil {
			return fmt.Errorf("deleting orphaned logical slot %q: %w", slot.SlotName, err)
		}
	}

	return nil
}
