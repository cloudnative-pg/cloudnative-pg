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

package roles

import (
	"context"
	"fmt"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// A RoleSynchronizer is a runnable that makes sure the Roles in the PostgreSQL databases
// are in sync with the spec
type RoleSynchronizer struct {
	instance *postgres.Instance
}

// NewRoleSynchronizer creates a new slot RoleSynchronizer
func NewRoleSynchronizer(instance *postgres.Instance) *RoleSynchronizer {
	runner := &RoleSynchronizer{
		instance: instance,
	}
	return runner
}

// Start starts running the slot RoleSynchronizer
func (sr *RoleSynchronizer) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx).WithName("RoleSynchronizer")
	contextLog.Info("XXXX starting up the runnable")
	isPrimary, err := sr.instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		contextLog.Info("XXXX skipping the role syncrhonization in replicas")
	}
	go func() {
		contextLog.Info("XXXX before got config")
		config := <-sr.instance.RoleSynchronizerChan()
		contextLog.Info("XXXX got config", "managedConfig", config)
		updateInterval := 1 * time.Minute // TODO: make configurable
		ticker := time.NewTicker(updateInterval)

		defer func() {
			ticker.Stop()
			contextLog.Info("Terminated RoleSynchronizer loop")
		}()

		for {
			contextLog.Info("XXXX synchronizing roles", "err", "none")
			select {
			case <-ctx.Done():
				return
			case config = <-sr.instance.RoleSynchronizerChan():
			case <-ticker.C:
			}

			// If replication is disabled stop the timer,
			// the process will resume through the wakeUp channel if necessary
			if config == nil || len(config.Roles) == 0 {
				ticker.Stop()
				// we set updateInterval to 0 to make sure the Ticker will be reset
				// if the feature is enabled again
				updateInterval = 0
				continue
			}

			// Update the ticker if the update interval has changed
			newUpdateInterval := updateInterval // TODO: make configurable
			if updateInterval != newUpdateInterval {
				ticker.Reset(newUpdateInterval)
				updateInterval = newUpdateInterval
			}

			err := sr.reconcile(ctx, config)
			if err != nil {
				contextLog.Info("synchronizing roles", "err", err)
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

func (sr *RoleSynchronizer) reconcile(ctx context.Context, config *apiv1.ManagedConfiguration) error {
	var err error

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from a panic: %s", r)
		}
	}()

	primaryPool, err := sr.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while reconciling managed roles: %w", err)
	}
	err = synchronizeRoles(
		ctx,
		NewPostgresRoleManager(primaryPool),
		sr.instance.PodName,
		config,
	)
	return err
}

// synchronizeRoles aligns roles in the database to the spec
func synchronizeRoles(
	ctx context.Context,
	roleManager RoleManager,
	podName string,
	config *apiv1.ManagedConfiguration,
) error {
	contextLog := log.FromContext(ctx).WithName("synchronizeRoles")
	contextLog.Info("XXXInvokedRoleSyncrhonizer",
		"primary", roleManager,
		"podName", podName,
		"config", config)

	rolesInDB, err := roleManager.List(ctx, config)
	if err != nil {
		return fmt.Errorf("while getting roles from primary: %v", err)
	}
	contextLog.Info("primaryRolesInDB", "rolesInDB", rolesInDB)

	rolesInSpec := config.Roles
	roleWithName := make(map[string]apiv1.RoleConfiguration)
	for _, r := range rolesInSpec {
		roleWithName[r.Name] = r
	}

	// 1. do any of the roles in the DB require update/delete?
	roleInDB := make(map[string]apiv1.RoleConfiguration)
	for _, role := range rolesInDB {
		roleInDB[role.Name] = role
		_, found := roleWithName[role.Name]
		if found {
			contextLog.Info("role in DB and Spec", "role", role.Name)
		} else {
			contextLog.Info("role in DB but not Spec", "role", role.Name)
		}
	}

	// 2. create managed roles that are not in the DB
	for _, r := range config.Roles {
		_, found := roleInDB[r.Name]
		if !found {
			contextLog.Info("Creating role in DB", "role", r.Name)
			err = roleManager.Create(ctx, r)
			if err != nil {
				return fmt.Errorf("while creating a role in the DB:: %w", err)
			}
		}
	}

	return nil
}
