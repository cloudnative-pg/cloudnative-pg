
package infrastructure

import (
	"context"
	"database/sql"
)

// DeleteLogicalSlot drops a logical replication slot by name
func DeleteLogicalSlot(ctx context.Context, db *sql.DB, slotName string) error {
	_, err := db.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slotName)
	return err
}

// ListLogicalSlotsWithSyncStatus lists logical replication slots with synced/failover/active status (PG17+)
func ListLogicalSlotsWithSyncStatus(ctx context.Context, db *sql.DB) (ReplicationSlotList, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT slot_name, slot_type, active, coalesce(restart_lsn::TEXT, '') AS restart_lsn,
				synced, failover, active
		 FROM pg_catalog.pg_replication_slots
		 WHERE slot_type = 'logical'`)
	if err != nil {
		return ReplicationSlotList{}, err
	}
	defer rows.Close()

	var status ReplicationSlotList
	for rows.Next() {
		var slot ReplicationSlot
		var synced, failover, active sql.NullBool
		err := rows.Scan(
			&slot.SlotName,
			&slot.Type,
			&slot.Active,
			&slot.RestartLSN,
			&synced,
			&failover,
			&active,
		)
		if err != nil {
			return ReplicationSlotList{}, err
		}
		if synced.Valid {
			slot.Synced = &synced.Bool
		}
		if failover.Valid {
			slot.Failover = &failover.Bool
		}
		if active.Valid {
			slot.Active = active.Bool
		}
		status.Items = append(status.Items, slot)
	}
	if rows.Err() != nil {
		return ReplicationSlotList{}, rows.Err()
	}
	return status, nil
}

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
	// PG17+ logical slot fields
	Synced     *bool    `json:"synced,omitempty"`   // nil if not PG17+ logical slot
	Failover   *bool    `json:"failover,omitempty"` // nil if not logical slot
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
