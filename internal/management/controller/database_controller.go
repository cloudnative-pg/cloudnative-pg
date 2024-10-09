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
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance instanceInterface
}

type instanceInterface interface {
	GetSuperUserDB() (*sql.DB, error)
	GetClusterName() string
	GetPodName() string
	GetNamespaceName() string
}

// errClusterIsReplica is raised when the database object
// cannot be reconciled because it belongs to a replica cluster
var errClusterIsReplica = fmt.Errorf("waiting for the cluster to become primary")

// databaseReconciliationInterval is the time between the
// database reconciliation loop failures
const databaseReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases/status,verbs=get;update;patch

// Reconcile is the database reconciliation loop
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the database object
	var database apiv1.Database
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &database); err != nil {
		// This is a deleted object, there's nothing
		// to do since we don't manage any finalizers.
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// This is not for me!
	if database.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return ctrl.Result{}, nil
	}

	// If everything is reconciled, we're done here
	if database.Generation == database.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, nil
	}

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		return r.failedReconciliation(
			ctx,
			&database,
			errClusterIsReplica,
		)
	}

	// Add the finalizer if we don't have it
	// nolint:nestif
	if database.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&database, utils.DatabaseFinalizerName) {
			if err := r.Update(ctx, &database); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// This database is being deleted
		if controllerutil.ContainsFinalizer(&database, utils.DatabaseFinalizerName) {
			if database.Spec.ReclaimPolicy == apiv1.DatabaseReclaimDelete {
				if err := r.deleteDatabase(ctx, &database); err != nil {
					return ctrl.Result{}, err
				}
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&database, utils.DatabaseFinalizerName)
			if err := r.Update(ctx, &database); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// Make sure the target PG Database is not being managed by another Database Object
	if err := r.ensureOnlyOneManager(ctx, database); err != nil {
		return r.failedReconciliation(
			ctx,
			&database,
			err,
		)
	}

	if err := r.reconcileDatabase(
		ctx,
		&database,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&database,
			err,
		)
	}

	return r.succeededReconciliation(
		ctx,
		&database,
	)
}

// ensureOnlyOneManager verifies that the target PostgreSQL Database specified by the given Database object
// is not already managed by another Database object within the same namespace and cluster.
// If another Database object is found to be managing the same PostgreSQL database, this method returns an error.
func (r *DatabaseReconciler) ensureOnlyOneManager(
	ctx context.Context,
	database apiv1.Database,
) error {
	contextLogger := log.FromContext(ctx)

	if database.Status.ObservedGeneration > 0 {
		return nil
	}

	var databaseList apiv1.DatabaseList
	if err := r.Client.List(ctx, &databaseList,
		client.InNamespace(r.instance.GetNamespaceName()),
	); err != nil {
		contextLogger.Error(err, "while getting database list", "namespace", r.instance.GetNamespaceName())
		return fmt.Errorf("impossible to list database objects in namespace %s: %w",
			r.instance.GetNamespaceName(), err)
	}

	for _, item := range databaseList.Items {
		if item.Name == database.Name {
			continue
		}

		if item.Spec.ClusterRef.Name != r.instance.GetClusterName() {
			continue
		}

		if item.Status.ObservedGeneration == 0 {
			continue
		}

		if item.Spec.Name == database.Spec.Name {
			return fmt.Errorf("database %q is already managed by Database object %q",
				database.Spec.Name, item.Name)
		}
	}

	return nil
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *DatabaseReconciler) failedReconciliation(
	ctx context.Context,
	database *apiv1.Database,
	err error,
) (ctrl.Result, error) {
	oldDatabase := database.DeepCopy()
	database.Status.Error = err.Error()
	database.Status.Ready = false

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contains the human
		// readable error
		database.Status.Error = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, database, client.MergeFrom(oldDatabase)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *DatabaseReconciler) succeededReconciliation(
	ctx context.Context,
	database *apiv1.Database,
) (ctrl.Result, error) {
	oldDatabase := database.DeepCopy()
	database.Status.Error = ""
	database.Status.Ready = true
	database.Status.ObservedGeneration = database.Generation

	if err := r.Client.Status().Patch(ctx, database, client.MergeFrom(oldDatabase)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// NewDatabaseReconciler creates a new database reconciler
func NewDatabaseReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *DatabaseReconciler {
	return &DatabaseReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
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
	var cluster apiv1.Cluster
	err := r.Client.Get(ctx,
		types.NamespacedName{
			Namespace: r.instance.GetNamespaceName(),
			Name:      r.instance.GetClusterName(),
		},
		&cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}

func (r *DatabaseReconciler) reconcileDatabase(ctx context.Context, obj *apiv1.Database) error {
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while connecting to the database %q: %w", obj.Spec.Name, err)
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

func (r *DatabaseReconciler) deleteDatabase(ctx context.Context, obj *apiv1.Database) error {
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while connecting to the database %q: %w", obj.Spec.Name, err)
	}

	return dropDatabase(ctx, db, obj)
}
