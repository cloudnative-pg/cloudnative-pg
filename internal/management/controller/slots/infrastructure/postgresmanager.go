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

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// List the available replication slots
func List(ctx context.Context, db *sql.DB, config *apiv1.ReplicationSlotsConfiguration) (ReplicationSlotList, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT slot_name, slot_type, active, coalesce(restart_lsn::TEXT, '') AS restart_lsn,
            xmin IS NOT NULL OR catalog_xmin IS NOT NULL AS holds_xmin
            FROM pg_catalog.pg_replication_slots
            WHERE NOT temporary AND slot_type = 'physical'`,
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
			&slot.HoldsXmin,
		)
		if err != nil {
			return ReplicationSlotList{}, err
		}

		slot.IsHA = strings.HasPrefix(slot.SlotName, config.HighAvailability.GetSlotPrefix())
		isFilteredByUser, err := config.SynchronizeReplicas.IsExcludedByUser(slot.SlotName)
		if err != nil {
			return status, err
		}
		if !slot.IsHA && isFilteredByUser {
			continue
		}

		status.Items = append(status.Items, slot)
	}

	if rows.Err() != nil {
		return ReplicationSlotList{}, rows.Err()
	}

	return status, nil
}

// Update the replication slot
func Update(ctx context.Context, db *sql.DB, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("updateSlot")
	contextLog.Trace("Invoked", "slot", slot)
	if slot.RestartLSN == "" {
		return nil
	}

	_, err := db.ExecContext(ctx, "SELECT pg_catalog.pg_replication_slot_advance($1, $2)", slot.SlotName, slot.RestartLSN)
	return err
}

// Create the replication slot
func Create(ctx context.Context, db *sql.DB, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("createSlot")
	contextLog.Trace("Invoked", "slot", slot)

	_, err := db.ExecContext(ctx, "SELECT pg_catalog.pg_create_physical_replication_slot($1, $2)",
		slot.SlotName, slot.RestartLSN != "")
	return err
}

// Delete the replication slot
func Delete(ctx context.Context, db *sql.DB, slot ReplicationSlot) error {
	contextLog := log.FromContext(ctx).WithName("dropSlot")
	contextLog.Trace("Invoked", "slot", slot)
	if slot.Active {
		return nil
	}

	_, err := db.ExecContext(ctx, "SELECT pg_catalog.pg_drop_replication_slot($1)", slot.SlotName)
	return err
}

// ListLogicalSlotsWithSyncStatus lists logical replication slots with their synced and failover status.
// The synced and failover columns are only available in PostgreSQL 17+; calling this on earlier versions
// will return a database error because the columns do not exist.
// Slots with synced=false were created locally; slots with synced=true were synchronized from the primary.
// Slots with failover=true are configured for failover synchronization.
func ListLogicalSlotsWithSyncStatus(ctx context.Context, db *sql.DB) ([]LogicalReplicationSlot, error) {
	contextLog := log.FromContext(ctx).WithName("listLogicalSlotsWithSyncStatus")

	rows, err := db.QueryContext(
		ctx,
		`SELECT slot_name, plugin, active, coalesce(restart_lsn::TEXT, '') AS restart_lsn, synced, failover
		FROM pg_catalog.pg_replication_slots
		WHERE slot_type = 'logical'`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pg_replication_slots for logical slots: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var slots []LogicalReplicationSlot
	for rows.Next() {
		var slot LogicalReplicationSlot
		err := rows.Scan(
			&slot.SlotName,
			&slot.Plugin,
			&slot.Active,
			&slot.RestartLSN,
			&slot.Synced,
			&slot.Failover,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning logical slot row: %w", err)
		}
		slots = append(slots, slot)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterating logical slot rows: %w", rows.Err())
	}

	contextLog.Trace("Listed logical slots with sync status", "count", len(slots))
	return slots, nil
}

// DeleteLogicalSlot drops a logical replication slot by name.
// Note: Active slots cannot be dropped - this will return an error from PostgreSQL.
func DeleteLogicalSlot(ctx context.Context, db *sql.DB, slotName string) error {
	contextLog := log.FromContext(ctx).WithName("deleteLogicalSlot")
	contextLog.Info("Dropping logical replication slot", "slotName", slotName)

	_, err := db.ExecContext(ctx, "SELECT pg_catalog.pg_drop_replication_slot($1)", slotName)
	if err != nil {
		return fmt.Errorf("executing pg_drop_replication_slot for %q: %w", slotName, err)
	}
	return nil
}
