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

// List the available replication slots
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
		`SELECT slot_name, slot_type, active, coalesce(restart_lsn::TEXT, '') FROM pg_replication_slots
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

	db, err := sm.pool.Connection("postgres")
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slot.SlotName)
	return err
}
