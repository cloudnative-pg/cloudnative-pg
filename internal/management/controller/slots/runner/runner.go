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

package runner

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"
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
		var config *apiv1.ReplicationSlotsConfiguration
		select {
		case config = <-sr.instance.SlotReplicatorChan():
		case <-ctx.Done():
			return
		}

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

			// If replication is disabled, stop the timer and perform cleanup
			if config == nil || !config.GetEnabled() {
				ticker.Stop()
				// We set updateInterval to 0 to make sure the Ticker will be reset
				// if the feature is enabled again
				updateInterval = 0
				// Perform cleanup before continuing
				if config != nil {
					if err := sr.reconcile(ctx, config); err != nil {
						contextLog.Warning("cleanup during reconcile when features are disabled", "err", err)
					}
				}
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

	contextLog := log.FromContext(ctx)

	if sr.instance.IsFenced() {
		contextLog.Trace("Replication slots reconciliation skipped: instance is fenced.")
		return nil
	}

	primaryPool := sr.instance.PrimaryConnectionPool()
	localPool := sr.instance.ConnectionPool()
	primaryDB, err := primaryPool.Connection("postgres")
	if err != nil {
		return err
	}
	localDB, err := localPool.Connection("postgres")
	if err != nil {
		return err
	}
	contextLog.Trace("Invoked",
		"primary", primaryPool.GetDsn("postgres"),
		"local", localPool.GetDsn("postgres"),
		"podName", sr.instance.GetPodName(),
		"config", config)
	err = synchronizeReplicationSlots(
		ctx,
		primaryDB,
		localDB,
		sr.instance.GetPodName(),
		config,
	)
	return err
}

// synchronizeReplicationSlots aligns the slots in the local instance with those in the primary
// nolint: gocognit
func synchronizeReplicationSlots(
	ctx context.Context,
	primaryDB *sql.DB,
	localDB *sql.DB,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) error {
	contextLog := log.FromContext(ctx).WithName("synchronizeReplicationSlots")

	slotsInPrimary, err := infrastructure.List(ctx, primaryDB, config)
	if err != nil {
		return fmt.Errorf("getting replication slot status from primary: %v", err)
	}
	contextLog.Trace("primary slot status", "slotsInPrimary", slotsInPrimary)

	slotsInLocal, err := infrastructure.List(ctx, localDB, config)
	if err != nil {
		return fmt.Errorf("getting replication slot status from local: %v", err)
	}
	contextLog.Trace("local slot status", "slotsInLocal", slotsInLocal)

	mySlotName := config.HighAvailability.GetSlotNameFromInstanceName(podName)

	for _, slot := range slotsInPrimary.Items {
		if slot.SlotName == mySlotName {
			continue
		}

		if slot.IsHA {
			if slot.RestartLSN == "" {
				continue
			}
			if !config.HighAvailability.GetEnabled() {
				continue
			}
		}

		if !slot.IsHA && !config.SynchronizeReplicas.GetEnabled() {
			continue
		}

		if !slotsInLocal.Has(slot.SlotName) {
			err := infrastructure.Create(ctx, localDB, slot)
			if err != nil {
				return err
			}
		}
		err := infrastructure.Update(ctx, localDB, slot)
		if err != nil {
			return err
		}
	}
	for _, slot := range slotsInLocal.Items {
		// Delete slots on standby with wrong state:
		//  * slots not present on the primary
		//  * the slot used by this node
		//  * slots holding xmin (this can happen on a former primary, and will prevent VACUUM from
		//      removing tuples deleted by any later transaction.)
		if !slotsInPrimary.Has(slot.SlotName) || slot.SlotName == mySlotName || slot.HoldsXmin {
			if err := infrastructure.Delete(ctx, localDB, slot); err != nil {
				return err
			}
			continue
		}

		// Drop orphaned logical replication slots with synced=false (PG 17+)
		// Only on replicas, not present on primary, not active, and not HA slot
		if slot.Type == "logical" && slot.Synced != nil && !*slot.Synced && !slot.Active {
			// Defensive: only drop if not present on primary and not HA
			if !slotsInPrimary.Has(slot.SlotName) && !slot.IsHA {
				if err := infrastructure.Delete(ctx, localDB, slot); err != nil {
					return err
				}
				continue
			}
		}

		// when the user turns off the feature we should delete all the created replication slots that aren't from HA
		if !slot.IsHA && !config.SynchronizeReplicas.GetEnabled() {
			if err := infrastructure.Delete(ctx, localDB, slot); err != nil {
				return err
			}
		}
	}

	return nil
}
