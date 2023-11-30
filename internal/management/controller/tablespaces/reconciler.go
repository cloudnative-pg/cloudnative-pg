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
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// Reconcile is the main reconciliation loop for the instance
func (r *TablespaceReconciler) Reconcile(
	ctx context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("tbs_reconciler")
	// if the context has already been cancelled,
	// trying to reconcile would just lead to misleading errors being reported
	if err := ctx.Err(); err != nil {
		contextLogger.Warning("Context cancelled, will not start tablespace reconcile", "err", err)
		return reconcile.Result{}, nil
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return reconcile.Result{}, err
	}
	if !isPrimary {
		contextLogger.Debug("skipping the tablespace reconciler in replicas")
		return reconcile.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	if !cluster.ContainsTablespaces() {
		contextLogger.Debug("no tablespaces to create")
		return reconcile.Result{}, nil
	}

	if r.instance.IsServerReady() != nil {
		contextLogger.Debug("database not ready, skipping tablespace reconciling")
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	contextLogger.Debug("starting up the tablespace reconciler")
	result, err := r.reconcile(ctx, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}
	if result != nil {
		return *result, nil
	}
	return reconcile.Result{}, nil
}

func (r *TablespaceReconciler) reconcile(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (*reconcile.Result, error) {
	superUserDB, err := r.instance.GetSuperUserDB()
	if err != nil {
		return nil, fmt.Errorf("while reconcile tablespaces: %w", err)
	}

	tbsManager := infrastructure.NewPostgresTablespaceManager(superUserDB)
	tbsStorageManager := instanceTablespaceStorageManager{}
	tbsInDatabase, err := tbsManager.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not fetch tablespaces from database: %w", err)
	}

	tbsByAction := evaluateNextActions(ctx, tbsInDatabase, cluster.Spec.Tablespaces)
	if len(tbsByAction.convertToTablespaceNameByStatus()[apiv1.TablespaceStatusPendingReconciliation]) == 0 {
		return nil, nil
	}

	// Reconcile the tablespaces into the database
	result, applyError := r.applyTablespaceActions(
		ctx,
		tbsManager,
		tbsStorageManager,
		tbsByAction,
	)
	if result != nil {
		return result, nil
	}

	// Update the status of the cluster
	tbsInDatabase, err = tbsManager.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not fetch tablespaces from database: %w", err)
	}
	tbsByAction = evaluateNextActions(ctx, tbsInDatabase, cluster.Spec.Tablespaces)
	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.TablespaceStatus.ByStatus = tbsByAction.convertToTablespaceNameByStatus()
	if err := r.GetClient().Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster)); err != nil {
		return nil, fmt.Errorf("while setting the tablespace reconciler status: %w", err)
	}

	return nil, applyError
}

// applyTablespaceActions applies the actions to reconcile tablespace in the DB with the Spec
func (r *TablespaceReconciler) applyTablespaceActions(
	ctx context.Context,
	tbsManager infrastructure.TablespaceManager,
	tbsStorageManager tablespaceStorageManager,
	tbsByAction TablespaceByAction,
) (*ctrl.Result, error) {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")

	for action, tbsAdapters := range tbsByAction {
		if action == TbsIsReconciled {
			contextLog.Debug("no action required", "action", action)
			continue
		}

		contextLog.Debug("tablespaces in database out of sync with in Spec, evaluating action",
			"tablespaces", getTablespaceNames(tbsAdapters), "action", action)

		switch action {
		case TbsToCreate:
			if result, err := r.applyCreateTablespace(
				ctx,
				tbsManager,
				tbsStorageManager,
				tbsAdapters,
			); result != nil || err != nil {
				return result, err
			}

		case TbsToUpdate:
			if err := r.applyUpdateTablespace(ctx, tbsManager, tbsAdapters); err != nil {
				return nil, err
			}

		default:
			contextLog.Error(errors.New("unsupported action"), "action", action)
		}
	}

	return nil, nil
}

func (r *TablespaceReconciler) applyCreateTablespace(
	ctx context.Context,
	tbsManager infrastructure.TablespaceManager,
	tbsStorageManager tablespaceStorageManager,
	tbsConfigs []TablespaceConfigurationAdapter,
) (*ctrl.Result, error) {
	contextLog := log.FromContext(ctx).WithName("tbs_create_reconciler")

	for _, tbs := range tbsConfigs {
		contextLog.Trace("creating tablespace ", "tablespace", tbs.Name)
		tbs := tbs
		tablespace := infrastructure.Tablespace{
			Name:  tbs.Name,
			Owner: tbs.Owner,
		}
		if exists, err := tbsStorageManager.storageExists(tbs.Name); err != nil || !exists {
			contextLog.Debug("deferring tablespace until creation of the mount point for the new volume",
				"tablespaceName", tbs.Name,
				"tablespacePath", tbsStorageManager.getStorageLocation(tbs.Name))
			return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		err := tbsManager.Create(ctx, tablespace)
		if err != nil {
			contextLog.Error(
				err, "while performing action",
				"tablespace", tbs.Name)
			return nil, err
		}
	}

	return nil, nil
}

func (r *TablespaceReconciler) applyUpdateTablespace(
	ctx context.Context,
	tbsManager infrastructure.TablespaceManager,
	tbsConfigs []TablespaceConfigurationAdapter,
) error {
	contextLog := log.FromContext(ctx).WithName("tbs_update_reconciler")

	for _, tbs := range tbsConfigs {
		contextLog.Trace("updating tablespace ", "tablespace", tbs.Name)
		tbs := tbs
		tablespace := infrastructure.Tablespace{
			Name:  tbs.Name,
			Owner: tbs.Owner,
		}
		err := tbsManager.Update(ctx, tablespace)
		if err != nil {
			contextLog.Error(
				err, "while performing action",
				"tablespace", tbs.Name)
			return err
		}
	}

	return nil
}
