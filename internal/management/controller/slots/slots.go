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
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

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

// Manager abstracts the operations that need to be sent to
// the database instance for the management of Replication Slots
type Manager interface {
	// List the available replication slots
	List(
		ctx context.Context,
		podName string,
		config *apiv1.ReplicationSlotsConfiguration,
	) (ReplicationSlotList, error)
	// Update the replication slot
	Update(ctx context.Context, slot ReplicationSlot) error
	// Create the replication slot
	Create(ctx context.Context, slot ReplicationSlot) error
	// Delete the replication slot
	Delete(ctx context.Context, slot ReplicationSlot) error
	GetCurrentHAReplicationSlots(instanceName string, cluster *apiv1.Cluster) (*ReplicationSlotList, error)
}

type connectionFactory func() (*sql.DB, error)

// PostgresManager is a Manager for a database instance
type PostgresManager struct {
	connFactory connectionFactory
}

// NewPostgresManager returns an implementation of Manager for postgres
func NewPostgresManager(factory connectionFactory) Manager {
	return PostgresManager{
		connFactory: factory,
	}
}

// List the available replication slots
func (sm PostgresManager) List(
	ctx context.Context,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) (ReplicationSlotList, error) {
	db, err := sm.connFactory()
	if err != nil {
		return ReplicationSlotList{}, err
	}

	rows, err := db.QueryContext(
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
			&slot.SlotName,
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

// Update the replication slot
func (sm PostgresManager) Update(ctx context.Context, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("updateSlot")
	contextLog.Trace("Invoked", "slot", slot)
	if slot.RestartLSN == "" {
		return nil
	}
	db, err := sm.connFactory()
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "SELECT pg_replication_slot_advance($1, $2)", slot.SlotName, slot.RestartLSN)
	return err
}

// Create the replication slot
func (sm PostgresManager) Create(ctx context.Context, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("createSlot")
	contextLog.Trace("Invoked", "slot", slot)

	db, err := sm.connFactory()
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "SELECT pg_create_physical_replication_slot($1, $2)",
		slot.SlotName, slot.RestartLSN != "")
	return err
}

// Delete the replication slot
func (sm PostgresManager) Delete(ctx context.Context, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("dropSlot")
	contextLog.Trace("Invoked", "slot", slot)
	if slot.Active {
		return nil
	}

	db, err := sm.connFactory()
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slot.SlotName)
	return err
}

// GetCurrentHAReplicationSlots retrieves the list of high availability replication slots
func (sm PostgresManager) GetCurrentHAReplicationSlots(
	instanceName string,
	cluster *apiv1.Cluster,
) (*ReplicationSlotList, error) {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil {
		return nil, fmt.Errorf("unexpected HA replication slots configuration")
	}

	db, err := sm.connFactory()
	if err != nil {
		return nil, err
	}

	var replicationSlots ReplicationSlotList

	rows, err := db.Query(
		`SELECT slot_name, slot_type, active FROM pg_replication_slots
            WHERE NOT temporary AND slot_name ^@ $1 AND slot_name != $2`,
		cluster.Spec.ReplicationSlots.HighAvailability.GetSlotPrefix(),
		cluster.GetSlotNameFromInstanceName(instanceName),
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

		replicationSlots.Items = append(replicationSlots.Items, slot)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return &replicationSlots, nil
}
