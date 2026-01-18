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

package tablespaces

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
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

	if err := r.instance.IsReady(); err != nil {
		contextLogger.Debug(
			"database not ready, skipping tablespace reconciling",
			"err", err)
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

func arePVCsForTablespaceHealthy(
	ctx context.Context,
	cluster *apiv1.Cluster,
	tablespaceName string,
) bool {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")

	healthyPVCs := cluster.Status.HealthyPVC
	isPVCHealthy := make(map[string]bool)
	for _, pvc := range healthyPVCs {
		isPVCHealthy[pvc] = true
	}
	instanceNames := cluster.Status.InstanceNames

	for _, instance := range instanceNames {
		pvcNameForTablespace := specs.PvcNameForTablespace(instance, tablespaceName)
		if !isPVCHealthy[pvcNameForTablespace] {
			contextLog.Warning("pvc unhealthy for tablespace",
				"pvcName", pvcNameForTablespace,
				"instance", instance,
				"tablespace", tablespaceName)
			return false
		}
	}

	return true
}

func (r *TablespaceReconciler) reconcile(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (*reconcile.Result, error) {
	superUserDB, err := r.instance.GetSuperUserDB()
	if err != nil {
		return nil, fmt.Errorf("while reconciling tablespaces: %w", err)
	}

	tbsInDatabase, err := infrastructure.List(ctx, superUserDB)
	if err != nil {
		return nil, fmt.Errorf("could not fetch tablespaces from database: %w", err)
	}

	pvcChecker := func(tablespace string) bool {
		return arePVCsForTablespaceHealthy(ctx, cluster, tablespace)
	}

	steps := evaluateNextSteps(ctx, tbsInDatabase, cluster.Spec.Tablespaces, pvcChecker)
	result := r.applySteps(
		ctx,
		superUserDB,
		steps,
	)

	// update the cluster status
	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.TablespacesStatus = result
	if err := r.client.Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster)); err != nil {
		return nil, fmt.Errorf("while setting the tablespace reconciler status: %w", err)
	}

	// if any tablespace is pending reconciliation, requeue
	for _, tbs := range updatedCluster.Status.TablespacesStatus {
		if tbs.State == apiv1.TablespaceStatusPendingReconciliation {
			return &reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}
	return nil, nil
}

// applySteps applies the actions to reconcile tablespaces in the DB with the Spec
// returns a collection of tablespace states, which may contain Postgres errors
// if they arose when applying the steps
func (r *TablespaceReconciler) applySteps(
	ctx context.Context,
	db *sql.DB,
	actions []tablespaceReconcilerStep,
) []apiv1.TablespaceState {
	result := make([]apiv1.TablespaceState, len(actions))

	for idx, step := range actions {
		result[idx] = step.execute(ctx, db, r.storageManager)
	}

	return result
}
