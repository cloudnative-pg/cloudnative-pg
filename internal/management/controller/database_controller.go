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

package controller

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance            instanceInterface
	finalizerReconciler *finalizerReconciler[*apiv1.Database]
	getSuperUserDB      func() (*sql.DB, error)
}

// databaseReconciliationInterval is the time between the
// database reconciliation loop failures
const databaseReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases/status,verbs=get;update;patch

// Reconcile is the database reconciliation loop
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).
		WithName("database_reconciler").
		WithValues("databaseName", req.Name)

	// Get the database object
	var database apiv1.Database
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &database); err != nil {
		contextLogger.Trace("Could not fetch Database", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// This is not for me!
	if database.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		contextLogger.Trace("Database is not for this cluster",
			"cluster", database.Spec.ClusterRef.Name,
			"expected", r.instance.GetClusterName(),
		)
		return ctrl.Result{}, nil
	}

	// If everything is reconciled, we're done here
	if database.Generation == database.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		return ctrl.Result{}, markAsFailed(ctx, r.Client, &database, fmt.Errorf("while fetching the cluster: %w", err))
	}

	contextLogger.Info("Reconciling database")
	defer func() {
		contextLogger.Info("Reconciliation loop of database exited")
	}()

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		if err := markAsUnknown(ctx, r.Client, &database, errClusterIsReplica); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	if err := r.finalizerReconciler.reconcile(ctx, &database); err != nil {
		return ctrl.Result{}, fmt.Errorf("while reconciling the finalizer: %w", err)
	}
	if !database.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	if res, err := detectConflictingManagers(ctx, r.Client, &database, &apiv1.DatabaseList{}); err != nil ||
		!res.IsZero() {
		return res, err
	}

	if err := r.reconcileDatabase(ctx, &database); err != nil {
		if markErr := markAsFailed(ctx, r.Client, &database, err); markErr != nil {
			contextLogger.Error(err, "while marking as failed the database resource",
				"error", err,
				"markError", markErr,
			)
			return ctrl.Result{}, fmt.Errorf(
				"encountered an error while marking as failed the database resource: %w, original error: %w",
				markErr,
				err)
		}
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	if err := markAsReady(ctx, r.Client, &database); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
}

func (r *DatabaseReconciler) evaluateDropDatabase(ctx context.Context, db *apiv1.Database) error {
	if db.Spec.ReclaimPolicy != apiv1.DatabaseReclaimDelete {
		return nil
	}
	sqlDB, err := r.getSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	return dropDatabase(ctx, sqlDB, db)
}

// NewDatabaseReconciler creates a new database reconciler
func NewDatabaseReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *DatabaseReconciler {
	dr := &DatabaseReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
		getSuperUserDB: func() (*sql.DB, error) {
			return instance.GetSuperUserDB()
		},
	}

	dr.finalizerReconciler = newFinalizerReconciler(
		mgr.GetClient(),
		utils.DatabaseFinalizerName,
		dr.evaluateDropDatabase,
	)

	return dr
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Database{}).
		Named("instance-database").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *DatabaseReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	return getClusterFromInstance(ctx, r.Client, r.instance)
}

func (r *DatabaseReconciler) reconcileDatabase(ctx context.Context, obj *apiv1.Database) error {
	db, err := r.getSuperUserDB()
	if err != nil {
		return fmt.Errorf("while connecting to the database %q: %w", obj.Spec.Name, err)
	}

	if obj.Spec.Ensure == apiv1.EnsureAbsent {
		return dropDatabase(ctx, db, obj)
	}

	dbExists, err := detectDatabase(ctx, db, obj)
	if err != nil {
		return fmt.Errorf("while detecting the database %q: %w", obj.Spec.Name, err)
	}

	if dbExists {
		return updateDatabase(ctx, db, obj)
	}

	return createDatabase(ctx, db, obj)
}
