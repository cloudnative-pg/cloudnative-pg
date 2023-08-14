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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// Reconcile triggers reconciliation of declarative tablespaces, gets their status, and
// updates it into the cluster Status
func Reconcile(
	ctx context.Context,
	instance *postgres.Instance,
	cluster *apiv1.Cluster,
	c client.Client,
) (reconcile.Result, error) {
	if instance.PodName != cluster.Status.CurrentPrimary ||
		cluster.Spec.Tablespaces == nil ||
		len(cluster.Spec.Tablespaces) == 0 {
		return reconcile.Result{}, nil
	}

	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Updating declarative tablespace information")

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return reconcile.Result{}, err
	}

	tbsMgr := infrastructure.NewPostgresTablespaceManager(db)
	tbsInDatabase, err := tbsMgr.List(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	tbsNameByStatus := EvaluateNextActions(ctx, tbsInDatabase, cluster.Spec.Tablespaces).
		convertToTablespaceNameByStatus()
	if len(tbsNameByStatus[apiv1.TablespaceStatusPendingReconciliation]) != 0 {
		// forces runnable to run
		instance.TriggerTablespaceSynchronizer(cluster.Spec.Tablespaces)
		contextLogger.Info("Triggered a tablespace reconciliation")
	}

	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.TablespaceStatus.ByStatus = tbsNameByStatus
	return reconcile.Result{}, c.Status().Patch(ctx, updatedCluster, client.MergeFrom(cluster))
}
