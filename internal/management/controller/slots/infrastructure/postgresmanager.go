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

package infrastructure

import (
	"context"
	"database/sql"
	"errors"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// pooler is an internal interface to pass a connection pooler to NewPostgresManager
type pooler interface {
	Connection(dbname string) (*sql.DB, error)
	GetDsn(dbname string) string
}

// PostgresManager is a Manager for a database instance
type PostgresManager struct {
	pool pooler
}

// NewPostgresManager returns an implementation of Manager for postgres
func NewPostgresManager(pool pooler) Manager {
	return PostgresManager{
		pool: pool,
	}
}

func (sm PostgresManager) String() string {
	return sm.pool.GetDsn("postgres")
}

// List the available managed physical replication slots
func (sm PostgresManager) List(
	ctx context.Context,
	config *v1.ReplicationSlotsConfiguration,
) (ReplicationSlotList, error) {
	db, err := sm.pool.Connection("postgres")
	if err != nil {
		return ReplicationSlotList{}, err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT slot_name, slot_type, active, coalesce(restart_lsn::TEXT, '') AS restart_lsn FROM pg_replication_slots
            WHERE NOT temporary AND slot_name ^@ $1 AND slot_type = 'physical'`,
		config.HighAvailability.GetSlotPrefix(),
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

// ListLogical lists the available logical replication slots
func (sm PostgresManager) ListLogical(
	ctx context.Context,
	config *v1.ReplicationSlotsConfiguration,
) (ReplicationSlotList, error) {
	db, err := sm.pool.Connection("postgres")
	if err != nil {
		return ReplicationSlotList{}, err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT slot_name, plugin, slot_type, active, coalesce(restart_lsn::TEXT, ''), two_phase AS restart_lsn FROM pg_replication_slots 
		   WHERE NOT temporary AND slot_type = 'logical'`,
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
			&slot.Plugin,
			&slot.Type,
			&slot.Active,
			&slot.RestartLSN,
			&slot.TwoPhase,
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
	db, err := sm.pool.Connection("postgres")
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

	db, err := sm.pool.Connection("postgres")
	if err != nil {
		return err
	}

	switch slot.Type {
	case SlotTypePhysical:
		_, err = db.ExecContext(ctx, "SELECT pg_create_physical_replication_slot($1, $2)",
			slot.SlotName, slot.RestartLSN != "")
	case SlotTypeLogical:
		_, err = db.ExecContext(ctx, "SELECT pg_create_logical_replication_slot($1, $2, $3, $4)",
			slot.SlotName, slot.Plugin, false, slot.TwoPhase)
	default:
		return errors.New("unsupported replication slot type")
	}

	return err
}

// GetState returns the state of the replication slot
func (sm PostgresManager) GetState(ctx context.Context, slot ReplicationSlot) ([]byte, error) {
	contextLog := log.FromContext(ctx).WithName("createSlot")
	contextLog.Trace("Invoked", "slot", slot)

	db, err := sm.pool.Connection("postgres")
	if err != nil {
		return nil, err
	}

	var state []byte
	err = db.QueryRowContext(
		ctx,
		`SELECT pg_catalog.pg_read_binary_file('pg_replslot/' || slot_name || '/state') FROM pg_catalog.pg_get_replication_slots() WHERE slot_name = $1`,
		slot.SlotName,
	).Scan(&state)

	return state, err
}

// Delete the replication slot
func (sm PostgresManager) Delete(ctx context.Context, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("dropSlot")
	contextLog.Trace("Invoked", "slot", slot)
	if slot.Active {
		return nil
	}

	db, err := sm.pool.Connection("postgres")
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slot.SlotName)
	return err
}
