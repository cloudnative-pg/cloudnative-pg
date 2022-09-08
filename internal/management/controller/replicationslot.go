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

package controller

import (
	"context"
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	postgresManagement "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// replicationSlotManager abstracts the operations that need to be sent to
// the database instance for the management of Replication Slots.
// This is so we can unit test the reconciliation logic vs. fake implementation
type replicationSlotManager interface {
	GetCurrentHAReplicationSlots(cluster *apiv1.Cluster) (*postgresManagement.ReplicationSlotList, error)
	CreateReplicationSlot(slotName string) error
	DeleteReplicationSlot(slotName string) error
}

func reconcileReplicationSlots(
	ctx context.Context,
	manager replicationSlotManager,
	isPrimary bool,
	cluster *apiv1.Cluster,
) error {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil {
		return nil
	}

	if isPrimary {
		return reconcilePrimaryReplicationSlots(ctx, manager, cluster)
	}
	return reconcileStandbyReplicationSlots(ctx, manager, cluster)
}

func reconcilePrimaryReplicationSlots(
	ctx context.Context,
	manager replicationSlotManager,
	cluster *apiv1.Cluster,
) error {
	// if the replication slots feature was deactivated, ensure any existing
	// replication slots get cleaned up
	if !cluster.Spec.ReplicationSlots.HighAvailability.Enabled {
		return dropPrimaryReplicationSlots(ctx, cluster)
	}

	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Updating primary HA replication slots")

	currentSlots, err := manager.GetCurrentHAReplicationSlots(cluster)
	if err != nil {
		return err
	}

	slotInCluster := make(map[postgresManagement.ReplicationSlot]bool)

	// Add every slot that is missing
	for _, instanceName := range cluster.Status.InstanceNames {
		if instanceName == cluster.Status.CurrentPrimary {
			continue
		}

		if slot := getSlotByInstanceName(currentSlots, instanceName, cluster); slot != nil {
			slotInCluster[*slot] = true
			continue
		}

		// at this point, the cluster instance does not have a replication slot
		slotName := cluster.GetSlotNameFromInstanceName(instanceName)
		if err := manager.CreateReplicationSlot(slotName); err != nil {
			return fmt.Errorf("updating primary HA replication slots: %w", err)
		}
		slotInCluster[postgresManagement.ReplicationSlot{
			SlotName: slotName,
			Type:     postgresManagement.SlotTypePhysical,
		}] = true
	}

	// Delete any replication slots in the Database that are not from a cluster instance
	for _, slot := range currentSlots.Items {
		if !slotInCluster[slot] {
			// Avoid deleting active slots.
			// It would trow an error on Postgres side.
			if slot.Active {
				continue
			}

			if err := manager.DeleteReplicationSlot(slot.SlotName); err != nil {
				return fmt.Errorf("failure deleting replication slot %q: %w", slot.SlotName, err)
			}
		}
	}

	return nil
}

// getSlotByInstanceName returns a slot searching by instance name
func getSlotByInstanceName(
	rs *postgresManagement.ReplicationSlotList,
	instanceName string,
	cluster *apiv1.Cluster,
) *postgresManagement.ReplicationSlot {
	if rs == nil || len(rs.Items) == 0 {
		return nil
	}

	for k, slot := range rs.Items {
		if cluster.GetInstanceNameFromSlotName(slot.SlotName) == instanceName {
			return &rs.Items[k]
		}
	}

	return nil
}

func dropPrimaryReplicationSlots(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("UNINPLEMENTED drop standby HA replication slots")
	// TODO: implement the logic to remove all the slots
	return nil
}

func reconcileStandbyReplicationSlots(
	ctx context.Context,
	manager replicationSlotManager,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Updating standby HA replication slots")

	// TODO: better logic must go here, for now we drop every slot that matches
	// as you can find on a former primary

	replicationSlots, err := manager.GetCurrentHAReplicationSlots(cluster)
	if err != nil {
		return err
	}

	for _, slot := range replicationSlots.Items {
		if err := manager.DeleteReplicationSlot(slot.SlotName); err != nil {
			// Avoid deleting active slots.
			// It would trow an error on Postgres side.
			if slot.Active {
				continue
			}

			return fmt.Errorf("deleting standby HA replication slots: %w", err)
		}
	}

	return nil
}
