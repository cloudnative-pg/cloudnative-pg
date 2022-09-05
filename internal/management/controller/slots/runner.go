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
	"fmt"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
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
			contextLog.Info("Terminated")
		}()
		defer func() {
			if r := recover(); r != nil {
				contextLog.Warning("Recovered from a panic", "value", r)
			}
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
			if config == nil || config.HighAvailability == nil || !config.HighAvailability.Enabled {
				ticker.Stop()
				continue
			}

			// Update the ticker if the update interval has changed
			newUpdateInterval := config.GetUpdateInterval()
			if updateInterval != newUpdateInterval {
				ticker.Reset(newUpdateInterval)
				updateInterval = newUpdateInterval
			}

			err := synchronizeReplicationSlots(
				ctx,
				sr.instance.PrimaryConnectionPool(),
				sr.instance.ConnectionPool(),
				sr.instance.PodName,
				config,
			)
			if err != nil {
				contextLog.Error(err, "synchronizing replication slots")
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

func synchronizeReplicationSlots(
	ctx context.Context,
	primaryPool *pool.ConnectionPool,
	localPool *pool.ConnectionPool,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) error {
	primaryDB, err := primaryPool.Connection("postgres")
	if err != nil {
		return err
	}

	localDB, err := localPool.Connection("postgres")
	if err != nil {
		return err
	}

	primaryStatus, err := getSlotsStatus(ctx, primaryDB, podName, config)
	if err != nil {
		return fmt.Errorf("getting replication slot status from primary: %v", err)
	}

	localStatus, err := getSlotsStatus(ctx, localDB, podName, config)
	if err != nil {
		return fmt.Errorf("getting replication slot status from primary: %v", err)
	}
	err = updateSlots(ctx, localDB, primaryStatus, localStatus)
	if err != nil {
		return fmt.Errorf("updateing replication slots: %v", err)
	}
	return nil
}
