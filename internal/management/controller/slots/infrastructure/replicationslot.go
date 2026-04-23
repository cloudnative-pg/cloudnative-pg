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

package infrastructure

// SlotType represents the type of replication slot
type SlotType string

// SlotTypePhysical represents the physical replication slot
const SlotTypePhysical SlotType = "physical"

// ReplicationSlot represents a single replication slot
type ReplicationSlot struct {
	SlotName   string   `json:"slotName,omitempty"`
	Type       SlotType `json:"type,omitempty"`
	Active     bool     `json:"active"`
	RestartLSN string   `json:"restartLSN,omitempty"`
	IsHA       bool     `json:"isHA,omitempty"`
	HoldsXmin  bool     `json:"holdsXmin,omitempty"`
}

// ReplicationSlotList contains a list of replication slots
type ReplicationSlotList struct {
	Items []ReplicationSlot
}

// Get returns the ReplicationSlot with the required name if present in the ReplicationSlotList
func (sl ReplicationSlotList) Get(name string) *ReplicationSlot {
	for i := range sl.Items {
		if sl.Items[i].SlotName == name {
			return &sl.Items[i]
		}
	}
	return nil
}

// Has returns true is a ReplicationSlot with the required name if present in the ReplicationSlotList
func (sl ReplicationSlotList) Has(name string) bool {
	return sl.Get(name) != nil
}

// LogicalReplicationSlot represents a logical replication slot for logical decoding.
// It parallels ReplicationSlot but captures logical-slot-specific fields
// like Plugin and Synced (PG17+) instead of physical-slot fields like IsHA.
type LogicalReplicationSlot struct {
	SlotName   string `json:"slotName"`             // The slot's unique identifier
	Plugin     string `json:"plugin,omitempty"`     // Output plugin (e.g., "pgoutput", "wal2json")
	Active     bool   `json:"active"`               // True if a consumer is connected to the slot
	RestartLSN string `json:"restartLSN,omitempty"` // WAL position from which the slot can start decoding
	Synced     bool   `json:"synced"`               // PG17+: false if locally created, true if synced from primary
	Failover   bool   `json:"failover"`             // PG17+: true if slot is configured for failover synchronization
}
