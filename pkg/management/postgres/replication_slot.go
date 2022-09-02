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

package postgres

import (
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// SlotType represents the type of replication slot
type SlotType string

// SlotTypePhysical represents the physical replication slot
const SlotTypePhysical SlotType = "physical"

// ReplicationSlot represents a single replication slot
// TODO - can the name be empty?
type ReplicationSlot struct {
	InstanceName string   `json:"instanceName,omitempty"`
	SlotName     string   `json:"slotName,omitempty"`
	Type         SlotType `json:"type,omitempty"`
	Active       bool     `json:"active"`
}

// ReplicationSlotList contains a list of replication slots
type ReplicationSlotList struct {
	Items []ReplicationSlot
}

// GetSlotByName returns a slot searching by slot name
func (rs *ReplicationSlotList) GetSlotByName(slotName string) *ReplicationSlot {
	if rs == nil || len(rs.Items) == 0 {
		return nil
	}

	for k, v := range rs.Items {
		if v.SlotName == slotName {
			return &rs.Items[k]
		}
	}

	return nil
}

// GetSlotByInstanceName returns a slot searching by instance name
func (rs *ReplicationSlotList) GetSlotByInstanceName(instanceName string) *ReplicationSlot {
	if rs == nil || len(rs.Items) == 0 {
		return nil
	}

	for k, v := range rs.Items {
		if v.InstanceName == instanceName {
			return &rs.Items[k]
		}
	}

	return nil
}

// GetCurrentHAReplicationSlots retrieves the list of high availability replication slots
func (instance *Instance) GetCurrentHAReplicationSlots(cluster *apiv1.Cluster) (*ReplicationSlotList, error) {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil {
		return nil, fmt.Errorf("unexpected HA replication slots configuration")
	}

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	var replicationSlots ReplicationSlotList

	rows, err := superUserDB.Query(
		`SELECT slot_name, slot_type, active FROM pg_replication_slots
            WHERE NOT temporary AND slot_name ^@ $1 AND slot_name != $2`,
		cluster.Spec.ReplicationSlots.HighAvailability.GetSlotPrefix(),
		cluster.GetSlotNameFromInstanceName(instance.PodName),
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var slot ReplicationSlot
		err := rows.Scan(
			&slot.SlotName,
			&slot.Type,
			&slot.Active,
		)
		if err != nil {
			return nil, err
		}

		slot.InstanceName = cluster.GetInstanceNameFromSlotName(slot.SlotName)

		replicationSlots.Items = append(replicationSlots.Items, slot)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return &replicationSlots, nil
}

// CreateReplicationSlot will create a physical replication slot in the primary instance
func (instance *Instance) CreateReplicationSlot(slotName string) error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	_, err = superUserDB.Exec("SELECT pg_create_physical_replication_slot($1)", slotName)
	if err != nil {
		return err
	}

	return nil
}

// DeleteReplicationSlot drop the specified replication slot in the primary
func (instance *Instance) DeleteReplicationSlot(slotName string) error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	_, err = superUserDB.Exec("SELECT pg_drop_replication_slot($1)", slotName)
	if err != nil {
		return err
	}

	return nil
}
