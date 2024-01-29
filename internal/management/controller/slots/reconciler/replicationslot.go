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

package reconciler

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// ReconcileReplicationSlots reconciles the replication slots of a given instance
func ReconcileReplicationSlots(
	ctx context.Context,
	instanceName string,
	manager infrastructure.Manager,
	cluster *apiv1.Cluster,
) (reconcile.Result, error) {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil {
		return reconcile.Result{}, nil
	}

	isPrimary := cluster.Status.CurrentPrimary == instanceName || cluster.Status.TargetPrimary == instanceName

	// if the ha replication slot is disabled, we drop all the HA replication slots
	// on both primary and standby
	// notes: if HA slots is enabled and user slot is disabled, drop user slot
	// is handled in the runner.go
	if !cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled() {
		return dropReplicationSlots(ctx, manager, cluster, isPrimary)
	}

	if isPrimary {
		return reconcilePrimaryReplicationSlots(ctx, manager, cluster)
	}

	return reconcile.Result{}, nil
}

// reconcilePrimaryReplicationSlots reconciles the HA replication slots of the primary instance
func reconcilePrimaryReplicationSlots(
	ctx context.Context,
	manager infrastructure.Manager,
	cluster *apiv1.Cluster,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Updating primary HA replication slots")

	currentSlots, err := manager.List(ctx, cluster.Spec.ReplicationSlots)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("reconciling primary replication slots: %w", err)
	}
	// expectedSlots is a map define the expected HA replication slots
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

		// at this point, the cluster instance does not have a HA replication slot
		if err := manager.Create(ctx, infrastructure.ReplicationSlot{SlotName: slotName}); err != nil {
			return reconcile.Result{}, fmt.Errorf("creating primary HA replication slots: %w", err)
		}
	}

	contextLogger.Trace("Status of primary HA replication slots",
		"currentSlots", currentSlots,
		"expectedSlots", expectedSlots)

	// Delete any HA replication slots in the instance that is not from an existing cluster instance
	needToReschedule := false
	for _, slot := range currentSlots.Items {
		if !expectedSlots[slot.SlotName] {
			// Avoid user-defined slot
			if !slot.IsHA {
				contextLogger.Trace("Skipping the user-defined replication slot",
					"slot", slot)
				continue
			}
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
			if err := manager.Delete(ctx, slot); err != nil {
				return reconcile.Result{}, fmt.Errorf("failure deleting replication slot %q: %w", slot.SlotName, err)
			}
		}
	}

	if needToReschedule {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	return reconcile.Result{}, nil
}

// drop the replication slots if HA or User replication slots is disabled.
// if HA replication slot is disabled, we drop all the HA replication slots on both primary and standby
// if User replication slot is disabled, we drop all the replication slots except the user replication slot on primary
func dropReplicationSlots(
	ctx context.Context,
	manager infrastructure.Manager,
	cluster *apiv1.Cluster,
	isPrimary bool,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)

	// if at the sametime, user replication slot is disabled too
	// we drop all the replication slots except the user replication slot on primary
	dropUserSlots := !cluster.Spec.ReplicationSlots.SynchronizeReplicas.GetEnabled()

	// we fetch all replication slots
	slots, err := manager.List(ctx, cluster.Spec.ReplicationSlots)
	if err != nil {
		return reconcile.Result{}, err
	}

	needToReschedule := false
	for _, slot := range slots.Items {
		// we always skip the user replication slots on the primary, which is user created
		if !slot.IsHA && isPrimary {
			continue
		}

		// if dropUserSlot is false, we skip the user replication slots
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
		if err := manager.Delete(ctx, slot); err != nil {
			return reconcile.Result{}, fmt.Errorf("while disabling standby HA replication slots: %w", err)
		}
	}

	if needToReschedule {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	return reconcile.Result{}, nil
}
