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

package tablespaces

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// A TablespaceSynchronizer is a Kubernetes manager.Runnable
// that makes sure the Tablespace in the PostgreSQL databases are in sync with the spec
//
// c.f. https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Runnable
type TablespaceSynchronizer struct {
	instance *postgres.Instance
	client   client.Client
}

// NewTablespaceSynchronizer creates a new TablespaceSynchronizer
func NewTablespaceSynchronizer(instance *postgres.Instance, client client.Client) *TablespaceSynchronizer {
	runner := &TablespaceSynchronizer{
		instance: instance,
		client:   client,
	}
	return runner
}

// Start starts running the TablespaceSynchronizer
func (tbsSync *TablespaceSynchronizer) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx).WithName("tablespaces_reconciler")
	contextLog.Info("starting up the runnable")
	isPrimary, err := tbsSync.instance.IsPrimary()
	if err != nil {
		return err
	}
	if !isPrimary {
		contextLog.Info("skipping the TablespaceSynchronizer in replicas")
	}
	go func() {
		var config map[string]*apiv1.TablespaceConfiguration
		contextLog.Info("setting up TablespaceSynchronizer loop")

		defer func() {
			contextLog.Info("Terminated TablespaceSynchronizer loop")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case config = <-tbsSync.instance.TablespaceSynchronizerChan():
			}
			contextLog.Debug("TablespaceSynchronizer loop triggered")

			// If the spec contains no tablespace to manage, stop the timer,
			// the process will resume through the wakeUp channel if necessary
			if len(config) == 0 {
				continue
			}

			err := tbsSync.reconcile(ctx, config)
			if err != nil {
				contextLog.Error(err, "synchronizing tablespace", "config", config)
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

// reconcile applied any necessary changes to the database to bring it in line
// with the spec. It also updates the cluster Status with the latest applied changes
func (tbsSync *TablespaceSynchronizer) reconcile(
	ctx context.Context,
	config map[string]*apiv1.TablespaceConfiguration,
) error {
	var err error
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from a panic: %s", r)
		}
	}()
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")
	contextLog.Debug("reconciling tablespace")

	if tbsSync.instance.IsServerHealthy() != nil {
		contextLog.Debug("database not ready, skipping tablespaces reconciling")
		return nil
	}

	superUserDB, err := tbsSync.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while reconciling managed roles: %w", err)
	}
	tbsManager := infrastructure.NewPostgresTablespaceManager(superUserDB)

	err = tbsSync.synchronizeTablespaces(ctx, tbsManager, config)
	if err != nil {
		return fmt.Errorf("while syncrhonizing tablespaces: %w", err)
	}
	return nil
}

// synchronizeTablespaces sync the tablespace in spec to database
func (tbsSync *TablespaceSynchronizer) synchronizeTablespaces(
	ctx context.Context,
	tbsManager infrastructure.TablespaceManager,
	config map[string]*apiv1.TablespaceConfiguration,
) error {
	tablespaceInDB, err := tbsManager.List(ctx)
	if err != nil {
		return err
	}
	tableSpaceByAction := EvaluateNextActions(ctx, tablespaceInDB, config)

	return tbsSync.applyTablespaceActions(
		ctx,
		tbsManager,
		tableSpaceByAction,
	)
}

// applyTablespaceActions applies the actions to reconcile tablespace in the DB with the Spec
func (tbsSync *TablespaceSynchronizer) applyTablespaceActions(
	ctx context.Context,
	roleManager infrastructure.TablespaceManager,
	tablespacesByAction TablespaceByAction,
) error {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")
	contextLog.Debug("applying tablespace actions")

	for action, tbsAdapters := range tablespacesByAction {
		switch action {
		case TbsIsReconciled, TbsReserved:
			contextLog.Debug("no action required", "action", action)
			continue
		}

		contextLog.Info("tablespace in DB out of sync with Spec, evaluating action",
			"tablespaces", getTablespaceNames(tbsAdapters), "action", action)

		for _, tbs := range tbsAdapters {
			err := tbsSync.applyTablespaceCreateUpdate(ctx, roleManager, tbs, action)
			if err != nil {
				contextLog.Error(err, "while performing "+string(action), "tablespace", tbs.Name)
			}
		}
	}

	return nil
}

// applyTablespaceCreate create or update tablespace
func (tbsSync *TablespaceSynchronizer) applyTablespaceCreateUpdate(
	ctx context.Context,
	tbsManager infrastructure.TablespaceManager,
	tbsAdapter TablespaceConfigurationAdapter,
	action TablespaceAction,
) error {
	tablespace := infrastructure.Tablespace{
		Name:      tbsAdapter.Name,
		Temporary: tbsAdapter.Temporary,
	}
	var err error
	switch action {
	case TbsToCreate:
		err = tbsManager.Create(ctx, tablespace)
	case TbsToUpdate:
		err = tbsManager.Update(ctx, tablespace)
	}

	return err
}
