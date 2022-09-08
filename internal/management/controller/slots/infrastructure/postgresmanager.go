package infrastructure

import (
	"context"
	"database/sql"
	"fmt"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

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
	config *v1.ReplicationSlotsConfiguration,
) (ReplicationSlotList, error) {
	db, err := sm.connFactory()
	if err != nil {
		return ReplicationSlotList{}, err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT slot_name, slot_type, active, coalesce(restart_lsn::text, '') as restart_lsn FROM pg_replication_slots
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
	cluster *v1.Cluster,
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
