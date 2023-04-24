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
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
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
