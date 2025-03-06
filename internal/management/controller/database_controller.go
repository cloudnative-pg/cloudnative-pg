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

	getSuperUserDB func() (*sql.DB, error)
	getTargetDB    func(dbname string) (*sql.DB, error)
}

// ErrFailedExtensionReconciliation is raised when an extension failed to reconcile
var ErrFailedExtensionReconciliation = fmt.Errorf("extension reconciliation failed")

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

	if err := r.reconcileDatabaseResource(ctx, &database); err != nil {
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
		getTargetDB: func(dbname string) (*sql.DB, error) {
			return instance.ConnectionPool().Connection(dbname)
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

func (r *DatabaseReconciler) reconcileDatabaseResource(ctx context.Context, obj *apiv1.Database) error {
	db, err := r.getSuperUserDB()
	if err != nil {
		return fmt.Errorf("while connecting to the database %q: %w", obj.Spec.Name, err)
	}

	if obj.Spec.Ensure == apiv1.EnsureAbsent {
		return dropDatabase(ctx, db, obj)
	}

	if err := r.reconcilePostgresDatabase(ctx, db, obj); err != nil {
		return err
	}

	obj.Status.Extensions = make([]apiv1.ExtensionStatus, len(obj.Spec.Extensions))
	for i := range obj.Spec.Extensions {
		ext := &obj.Spec.Extensions[i]
		obj.Status.Extensions[i] = r.reconcileDatabaseExtension(
			ctx,
			obj.Spec.Name,
			ext,
		)
	}

	if !areAllExtensionsApplied(obj.Status.Extensions) {
		return ErrFailedExtensionReconciliation
	}

	return nil
}

func (r *DatabaseReconciler) reconcilePostgresDatabase(ctx context.Context, db *sql.DB, obj *apiv1.Database) error {
	dbExists, err := detectDatabase(ctx, db, obj)
	if err != nil {
		return fmt.Errorf("while detecting the database %q: %w", obj.Spec.Name, err)
	}

	if dbExists {
		return updateDatabase(ctx, db, obj)
	}

	return createDatabase(ctx, db, obj)
}

func (r *DatabaseReconciler) reconcileDatabaseExtension(
	ctx context.Context,
	dbname string,
	ext *apiv1.ExtensionSpec,
) apiv1.ExtensionStatus {
	db, err := r.getTargetDB(dbname)
	if err != nil {
		return apiv1.ExtensionStatus{
			Name:    ext.Name,
			Applied: false,
			Message: fmt.Sprintf("while connecting to the database %q: %v", dbname, err),
		}
	}

	extensionInfo, err := getDatabaseExtensionInfo(ctx, db, ext)
	if err != nil {
		return apiv1.ExtensionStatus{
			Name:    ext.Name,
			Applied: false,
			Message: fmt.Sprintf("while detecting the extension %q: %v", ext.Name, err),
		}
	}

	extensionExists := extensionInfo != nil

	switch {
	case !extensionExists && ext.Ensure == apiv1.EnsurePresent:
		return reconcileCreateDatabaseExtension(ctx, db, ext)

	case !extensionExists && ext.Ensure == apiv1.EnsureAbsent:
		return apiv1.ExtensionStatus{
			Name:    ext.Name,
			Applied: true,
		}

	case extensionExists && ext.Ensure == apiv1.EnsurePresent:
		return reconcileUpdateDatabaseExtension(ctx, db, ext, extensionInfo)

	case extensionExists && ext.Ensure == apiv1.EnsureAbsent:
		return reconcileDropDatabaseExtension(ctx, db, ext)

	default:
		// If this happens, the CRD and/or the validating webhook
		// are not working properly. In this case, let's do nothing:
		// better to be safe than sorry.
		return apiv1.ExtensionStatus{
			Name:    ext.Name,
			Applied: true,
		}
	}
}

func areAllExtensionsApplied(extensionStatus []apiv1.ExtensionStatus) bool {
	for i := range extensionStatus {
		if !extensionStatus[i].Applied {
			return false
		}
	}

	return true
}

func reconcileCreateDatabaseExtension(ctx context.Context, db *sql.DB, ext *apiv1.ExtensionSpec) apiv1.ExtensionStatus {
	if err := createDatabaseExtension(ctx, db, ext); err != nil {
		return apiv1.ExtensionStatus{
			Name:    ext.Name,
			Applied: false,
			Message: err.Error(),
		}
	}

	return apiv1.ExtensionStatus{
		Name:    ext.Name,
		Applied: true,
	}
}

func reconcileDropDatabaseExtension(ctx context.Context, db *sql.DB, ext *apiv1.ExtensionSpec) apiv1.ExtensionStatus {
	if err := dropDatabaseExtension(ctx, db, ext); err != nil {
		return apiv1.ExtensionStatus{
			Name:    ext.Name,
			Applied: false,
			Message: err.Error(),
		}
	}

	return apiv1.ExtensionStatus{
		Name:    ext.Name,
		Applied: true,
	}
}

func reconcileUpdateDatabaseExtension(
	ctx context.Context, db *sql.DB, spec *apiv1.ExtensionSpec, info *extInfo,
) apiv1.ExtensionStatus {
	if err := updateDatabaseExtension(ctx, db, spec, info); err != nil {
		return apiv1.ExtensionStatus{
			Name:    spec.Name,
			Applied: false,
			Message: err.Error(),
		}
	}

	return apiv1.ExtensionStatus{
		Name:    spec.Name,
		Applied: true,
	}
}
