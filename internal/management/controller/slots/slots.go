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

package slots

import (
	"context"
	"database/sql"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ReplicationSlot represents a single replication slot
type ReplicationSlot struct {
	Name       string `json:"slotName,omitempty"`
	Type       string `json:"type,omitempty"`
	Active     bool   `json:"active"`
	RestartLSN string `json:"restartLSN,omitempty"`
}

// ReplicationSlotList contains a list of replication slots
type ReplicationSlotList struct {
	Items []ReplicationSlot
}

// Get returns the ReplicationSlot with the required name if present in the ReplicationSlotList
func (sl ReplicationSlotList) Get(name string) *ReplicationSlot {
	for i := range sl.Items {
		if sl.Items[i].Name == name {
			return &sl.Items[i]
		}
	}
	return nil
}

// Has returns true is a ReplicationSlot with the required name if present in the ReplicationSlotList
func (sl ReplicationSlotList) Has(name string) bool {
	return sl.Get(name) != nil
}

// slotManager abstracts the operations that need to be sent to
// the database instance for the management of Replication Slots
type slotManager interface {
	getSlotsStatus(
		ctx context.Context,
		podName string,
		config *apiv1.ReplicationSlotsConfiguration,
	) (ReplicationSlotList, error)
	updateSlot(ctx context.Context, slot ReplicationSlot) error
	createSlot(ctx context.Context, slot ReplicationSlot) error
	dropSlot(ctx context.Context, slot ReplicationSlot) error
}

// dbSlotManager is a slotManager for a database instance
type dbSlotManager struct {
	db *sql.DB
}

func getDBSlotManager(db *sql.DB) slotManager {
	return dbSlotManager{
		db: db,
	}
}

func (sm dbSlotManager) getSlotsStatus(
	ctx context.Context,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) (ReplicationSlotList, error) {
	rows, err := sm.db.QueryContext(
		ctx,
		`SELECT slot_name, slot_type, active, restart_lsn FROM pg_replication_slots
            WHERE NOT temporary AND slot_name ^@ $1 AND slot_name != $2 AND slot_type = 'physical'`,
		config.HighAvailability.GetSlotPrefix(),
		config.HighAvailability.GetSlotNameFromInstanceName(podName),
	)
	if err != nil {
		return ReplicationSlotList{}, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var status ReplicationSlotList
	for rows.Next() {
		var slot ReplicationSlot
		err := rows.Scan(
			&slot.Name,
			&slot.Type,
			&slot.Active,
			&slot.RestartLSN,
		)
		if err != nil {
			return ReplicationSlotList{}, err
		}

		status.Items = append(status.Items, slot)
	}

	if rows.Err() != nil {
		return ReplicationSlotList{}, rows.Err()
	}

	return status, nil
}

func (sm dbSlotManager) updateSlot(ctx context.Context, slot ReplicationSlot) error {
	if slot.RestartLSN == "" {
		return nil
	}

	_, err := sm.db.ExecContext(ctx, "SELECT pg_replication_slot_advance($1, $2)", slot.Name, slot.RestartLSN)
	return err
}

func (sm dbSlotManager) createSlot(ctx context.Context, slot ReplicationSlot) error {
	_, err := sm.db.ExecContext(ctx, "SELECT pg_create_physical_replication_slot($1, $2)",
		slot.Name, slot.RestartLSN != "")
	return err
}

func (sm dbSlotManager) dropSlot(ctx context.Context, slot ReplicationSlot) error {
	if slot.Active {
		return nil
	}
	_, err := sm.db.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slot.Name)
	return err
}
