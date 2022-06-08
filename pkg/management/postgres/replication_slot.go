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
	"reflect"
	"regexp"

	"github.com/lib/pq"
)

// SlotType represents the type of replication slot
type SlotType string

// SlotTypePhysical represents the physical replication slot
const SlotTypePhysical = "physical"

// SlotPrefix is the prefix we use to create our slots
const SlotPrefix = "_cnpg_slot_"

// ReplicationSlot represent the unit of a replication slot
type ReplicationSlot struct {
	PodName  string   `json:"podName,omitempty"`
	SlotName string   `json:"slotName,omitempty"`
	Type     SlotType `json:"type,omitempty"`
}

// ReplicationSlotList contains a list of replication slot
type ReplicationSlotList struct {
	ClusterName string `json:"clusterName,omitempty"`
	Items       []ReplicationSlot
}

var (
	podSerialNumber  = regexp.MustCompile(".*-(?P<wat>[0-9]+)$")
	slotSerialNumber = regexp.MustCompile(".*_(?P<wat>[0-9]+)$")
)

// GetSlotName return the slot name based in the current pod name
func GetSlotName(podName string) (string, error) {
	match := podSerialNumber.FindStringSubmatch(podName)
	if len(match) != 2 {
		return "", fmt.Errorf("can't parse podName looking for serial number")
	}
	slotName := fmt.Sprintf("%s%s", SlotPrefix, match[1])

	return slotName, nil
}

func (rs *ReplicationSlotList) getPodNameBySlot(slotName string) (string, error) {
	match := slotSerialNumber.FindStringSubmatch(slotName)
	if len(match) != 2 {
		return "", fmt.Errorf("can't parse slot name looking for serial number")
	}
	podName := fmt.Sprintf("%s-%s", rs.ClusterName, match[1])

	return podName, nil
}

func (rs *ReplicationSlotList) getSlotByPodName(podName string) *ReplicationSlot {
	if rs == nil || len(rs.Items) == 0 {
		return nil
	}
	for k, v := range rs.Items {
		if v.PodName == podName {
			return &rs.Items[k]
		}
	}
	return nil
}

// Has returns true if the slotName it's found in the current replication slot list
func (rs *ReplicationSlotList) Has(podNAme string) bool {
	return rs.getSlotByPodName(podNAme) != nil
}

// TODO compare against the active nodes in the cluster status
func (instance *Instance) getCurrentReplicationSlot() (*ReplicationSlotList, error) {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	replicationSlots := ReplicationSlotList{
		ClusterName: instance.ClusterName,
	}

	rows, err := superUserDB.Query(
		`SELECT
slot_name,
slot_type
FROM pg_replication_slots 
WHERE NOT temporary AND slot_type = 'physical'
`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	for rows.Next() {
		var slot ReplicationSlot
		err := rows.Scan(
			&slot.SlotName,
			&slot.Type,
		)
		if err != nil {
			return nil, err
		}
		slot.PodName, err = replicationSlots.getPodNameBySlot(slot.SlotName)
		if err != nil {
			return nil, err
		}

		replicationSlots.Items = append(replicationSlots.Items, slot)
	}

	return &replicationSlots, nil
}

// UpdateReplicationsSlot will update the ReplicationSlots list in the instance list
func (instance *Instance) UpdateReplicationsSlot() error {
	if isPrimary, _ := instance.IsPrimary(); !isPrimary {
		return nil
	}
	replicationslots, err := instance.getCurrentReplicationSlot()
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(instance.ReplicationSlots, replicationslots) {
		instance.ReplicationSlots = replicationslots
	}

	return nil
}

// CreateReplicationSlot will create a physical replication slot in the primary instance
func (instance *Instance) CreateReplicationSlot(podName string) error {
	if isPrimary, _ := instance.IsPrimary(); !isPrimary {
		return nil
	}

	slotName, err := GetSlotName(podName)
	if err != nil {
		return err
	}

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	row := superUserDB.QueryRow("SELECT * FROM pg_create_physical_replication_slot('$1')", slotName)
	if row.Err() != nil {
		return err
	}

	instance.ReplicationSlots.Items = append(instance.ReplicationSlots.Items,
		ReplicationSlot{
			PodName:  podName,
			SlotName: slotName,
			Type:     SlotTypePhysical,
		})

	return nil
}

// DeleteReplicationSlot drop the specified replication slot in the primary
func (instance *Instance) DeleteReplicationSlot(podName string) error {
	if isPrimary, _ := instance.IsPrimary(); !isPrimary {
		return nil
	}

	slotName, err := GetSlotName(podName)
	if err != nil {
		return err
	}

	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}

	_, err = superUserDB.Exec(fmt.Sprintf(
		"SELECT pg_drop_replication_slot('%s')",
		pq.QuoteIdentifier(slotName)))
	if err != nil {
		return err
	}

	return nil
}
