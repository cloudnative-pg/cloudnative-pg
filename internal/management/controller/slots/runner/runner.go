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

package runner

import (
	"context"
	"fmt"
	"os"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// A Replicator is a runner that keeps replication slots in sync between the primary and this replica
type Replicator struct {
	instance *postgres.Instance
}

// NewReplicator creates a new slot Replicator
func NewReplicator(instance *postgres.Instance) *Replicator {
	runner := &Replicator{
		instance: instance,
	}
	return runner
}

// Start starts running the slot Replicator
func (sr *Replicator) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx).WithName("Replicator")
	go func() {
		config := <-sr.instance.SlotReplicatorChan()
		updateInterval := config.GetUpdateInterval()
		ticker := time.NewTicker(updateInterval)

		defer func() {
			ticker.Stop()
			contextLog.Info("Terminated slot Replicator loop")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case config = <-sr.instance.SlotReplicatorChan():
			case <-ticker.C:
			}

			// If replication is disabled stop the timer,
			// the process will resume through the wakeUp channel if necessary
			if config == nil || config.HighAvailability == nil || !config.HighAvailability.GetEnabled() {
				ticker.Stop()
				// we set updateInterval to 0 to make sure the Ticker will be reset
				// if the feature is enabled again
				updateInterval = 0
				continue
			}

			// Update the ticker if the update interval has changed
			newUpdateInterval := config.GetUpdateInterval()
			if updateInterval != newUpdateInterval {
				ticker.Reset(newUpdateInterval)
				updateInterval = newUpdateInterval
			}

			err := sr.reconcile(ctx, config)
			if err != nil {
				contextLog.Warning("synchronizing replication slots", "err", err)
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

func (sr *Replicator) reconcile(ctx context.Context, config *apiv1.ReplicationSlotsConfiguration) error {
	var err error

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from a panic: %s", r)
		}
	}()

	primaryPool := sr.instance.PrimaryConnectionPool()
	localPool := sr.instance.ConnectionPool()
	err = synchronizeReplicationSlots(
		ctx,
		infrastructure.NewPostgresManager(primaryPool),
		infrastructure.NewPostgresManager(localPool),
		sr.instance.PodName,
		config,
	)
	if err != nil {
		return err
	}

	err = sr.synchronizeLogicalReplicationSlots(
		ctx,
		infrastructure.NewPostgresManager(primaryPool),
		infrastructure.NewPostgresManager(localPool),
		config,
	)

	return err
}

// synchronizeReplicationSlots aligns the slots in the local instance with those in the primary
func synchronizeReplicationSlots(
	ctx context.Context,
	primarySlotManager infrastructure.Manager,
	localSlotManager infrastructure.Manager,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) error {
	contextLog := log.FromContext(ctx).WithName("synchronizeReplicationSlots")
	contextLog.Trace("Invoked",
		"primary", primarySlotManager,
		"local", localSlotManager,
		"podName", podName,
		"config", config)

	slotsInPrimary, err := primarySlotManager.List(ctx, config)
	if err != nil {
		return fmt.Errorf("getting replication slot status from primary: %v", err)
	}
	contextLog.Trace("primary slot status", "slotsInPrimary", slotsInPrimary)

	slotsInLocal, err := localSlotManager.List(ctx, config)
	if err != nil {
		return fmt.Errorf("getting replication slot status from local: %v", err)
	}
	contextLog.Trace("local slot status", "slotsInLocal", slotsInLocal)

	mySlotName := config.HighAvailability.GetSlotNameFromInstanceName(podName)

	for _, slot := range slotsInPrimary.Items {
		if slot.SlotName == mySlotName {
			continue
		}
		if slot.RestartLSN == "" {
			continue
		}
		if !slotsInLocal.Has(slot.SlotName) {
			err := localSlotManager.Create(ctx, slot)
			if err != nil {
				return err
			}
		}
		err := localSlotManager.Update(ctx, slot)
		if err != nil {
			return err
		}
	}
	for _, slot := range slotsInLocal.Items {
		if !slotsInPrimary.Has(slot.SlotName) || slot.SlotName == mySlotName {
			err := localSlotManager.Delete(ctx, slot)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// synchronizeReplicationSlots aligns the slots in the local instance with those in the primary
func (sr *Replicator) synchronizeLogicalReplicationSlots(
	ctx context.Context,
	primarySlotManager infrastructure.Manager,
	localSlotManager infrastructure.Manager,
	config *apiv1.ReplicationSlotsConfiguration,
) error {
	contextLog := log.FromContext(ctx).WithName("synchronizeLogicalReplicationSlots")
	contextLog.Trace("Invoked",
		"primary", primarySlotManager,
		"local", localSlotManager,
		"podName", sr.instance.PodName,
		"config", config)

	logicalSlotsInPrimary, err := primarySlotManager.ListLogical(ctx, config)
	if err != nil {
		return fmt.Errorf("getting logical replication slot status from primary: %v", err)
	}
	contextLog.Trace("primary logical slot status", "logicalSlotsInPrimary", logicalSlotsInPrimary)

	logicalSlotsInLocal, err := localSlotManager.ListLogical(ctx, config)
	if err != nil {
		return fmt.Errorf("getting logical replication slot status from local: %v", err)
	}
	contextLog.Trace("local logical slot status", "logicalSlotsInLocal", logicalSlotsInLocal)

	restart := false
	for _, slot := range logicalSlotsInPrimary.Items {
		if !logicalSlotsInLocal.Has(slot.SlotName) {
			requireRestart, err := sr.createLogicalReplicationSlot(ctx, primarySlotManager, localSlotManager, slot)
			if err != nil {
				return err
			}
			restart = restart || requireRestart
		}
	}

	// Instance requires a restart for logical replication slots to be picked up if they were created through cloning
	if restart {
		err := sr.instance.RequestAndWaitRestartSmartFast()
		if err != nil {
			return err
		}
	}

	for _, slot := range logicalSlotsInPrimary.Items {
		if slot.RestartLSN == "" {
			continue
		}

		err := localSlotManager.Update(ctx, slot)
		if err != nil {
			return err
		}
	}

	for _, slot := range logicalSlotsInLocal.Items {
		if !logicalSlotsInPrimary.Has(slot.SlotName) {
			err := localSlotManager.Delete(ctx, slot)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (sr *Replicator) createLogicalReplicationSlot(
	ctx context.Context,
	primarySlotManager infrastructure.Manager,
	localSlotManager infrastructure.Manager,
	slot infrastructure.ReplicationSlot) (bool, error) {
	contextLog := log.FromContext(ctx).WithName("createLogicalReplicationSlot")
	contextLog.Trace("Invoked",
		"primary", primarySlotManager,
		"local", localSlotManager)

	pgVersion, err := sr.instance.GetPgVersion()
	if err != nil {
		return false, err
	}

	// Since the introduction of logical replication slot creation in v16 we can just create the slot via the SQL layer
	if pgVersion.Major >= 16 {
		err := localSlotManager.Create(ctx, slot)
		return false, err
	}

	// Ensure the instance is fully ready before performing state cloning to create the replication slot
	err = sr.instance.IsServerReady()
	if err != nil {
		contextLog.Warning("skipping logical replication slot cloning - server is not ready", err)
		return false, err
	}

	// In version <16 we cannot create logical replication slots via the sql layer,
	// we have to clone the state from the primary directly into the standby
	state, err := primarySlotManager.GetState(ctx, slot)
	if err != nil {
		return false, err
	}
	pgDataPath := specs.PgDataPath
	if envDataPath, ok := os.LookupEnv("PGDATA"); ok {
		pgDataPath = envDataPath
	}
	contextLog.Trace("State",
		"primary", primarySlotManager,
		"pgDataPath", pgDataPath,
		"state", state)
	err = os.MkdirAll(fmt.Sprintf("%s/pg_replslot/%s", pgDataPath, slot.SlotName), 0o700)
	if err != nil && !os.IsExist(err) {
		return false, err
	}

	err = os.WriteFile(fmt.Sprintf("%s/pg_replslot/%s/state", pgDataPath, slot.SlotName), state, 0o600)
	if err != nil {
		return false, err
	}

	return true, nil
}
