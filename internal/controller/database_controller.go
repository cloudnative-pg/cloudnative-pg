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
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instanceClient instance.Client
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases/status,verbs=get;update;patch

// errClusterNotPresent is raised when the database object
// refers to a non-existing cluster
var errClusterNotPresent = fmt.Errorf("cluster not present")

// errClusterIsReplica is raised when the database object
// cannot be reconciled because it belongs to a replica cluster
var errClusterIsReplica = fmt.Errorf("waiting for the cluster to become primary")

// errClusterPrimaryNotStable is raised when the cluster still
// do not have a stable primary instance and the reconciliation
// need to be retried
var errClusterPrimaryNotStable = fmt.Errorf("waiting for a stable primary instance")

// databaseReconciliationInterval is the time between the
// database reconciliation loop failures
const databaseReconciliationInterval = 30 * time.Second

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Database object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
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
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// If everything is reconciled, we're done here
	if database.Generation == database.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Let's get the corresponding Cluster object
	var cluster apiv1.Cluster
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: database.Namespace,
		Name:      database.Spec.ClusterRef.Name,
	}, &cluster); err != nil {
		if apierrs.IsNotFound(err) {
			return r.failedReconciliation(
				ctx,
				&database,
				errClusterNotPresent,
			)
		}
		return ctrl.Result{}, err
	}

	if cluster.IsReplica() {
		return r.failedReconciliation(
			ctx,
			&database,
			errClusterIsReplica,
		)
	}

	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary ||
		len(cluster.Status.CurrentPrimary) == 0 {
		return r.failedReconciliation(
			ctx,
			&database,
			errClusterPrimaryNotStable,
		)
	}

	// Get the reference to the primary pod
	var primaryPod corev1.Pod
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: database.Namespace,
		Name:      cluster.Status.CurrentPrimary,
	}, &primaryPod); err != nil {
		return r.failedReconciliation(
			ctx,
			&database,
			fmt.Errorf("while getting primary pod: %w", err),
		)
	}

	// Store in the context the TLS configuration required communicating with the Pods
	ctx, err := certs.NewTLSConfigForContext(
		ctx,
		r.Client,
		cluster.GetServerCASecretObjectKey(),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	dbRequest := instance.PgDatabase{
		Owner:            database.Spec.Owner,
		Encoding:         database.Spec.Encoding,
		IsTemplate:       database.Spec.IsTemplate,
		AllowConnections: database.Spec.AllowConnections,
		ConnectionLimit:  database.Spec.ConnectionLimit,
		Tablespace:       database.Spec.Tablespace,
	}

	if err := r.instanceClient.PostDatabase(
		ctx,
		&primaryPod,
		database.Spec.Name,
		dbRequest,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&database,
			err,
		)
	}

	return r.succeededRenconciliation(
		ctx,
		&database,
	)
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
		// The body line of the instance manager contain the human
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
func (r *DatabaseReconciler) succeededRenconciliation(
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

// NewDatabaseReconciler creates a new databare reconciler
func NewDatabaseReconciler(
	mgr manager.Manager,
) *DatabaseReconciler {
	return &DatabaseReconciler{
		Client:         mgr.GetClient(),
		instanceClient: instance.NewStatusClient(),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Database{}).
		Complete(r)
}
