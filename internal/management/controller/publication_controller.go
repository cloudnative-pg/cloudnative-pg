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
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PublicationReconciler reconciles a Publication object
type PublicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance *postgres.Instance
}

// publicationReconciliationInterval is the time between the
// publication reconciliation loop failures
const publicationReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Publication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *PublicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the publication object
	var publication apiv1.Publication
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &publication); err != nil {
		contextLogger.Trace("Could not fetch Publication", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// This is not for me!
	if publication.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		contextLogger.Trace("Publication is not for this cluster",
			"cluster", publication.Spec.ClusterRef.Name,
			"expected", r.instance.GetClusterName(),
		)
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		return ctrl.Result{}, markAsFailed(ctx, r.Client, &publication, fmt.Errorf("while fetching the cluster: %w", err))
	}

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
		markErr := markAsFailed(ctx, r.Client, &publication, errClusterIsReplica)
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, markErr
	}

	if err := r.reconcileFinalizer(ctx, publication); err != nil {
		return ctrl.Result{}, fmt.Errorf("while reconciling the finalizer: %w", err)
	}

	// If everything is reconciled, we're done here
	if publication.Generation == publication.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	if err := r.alignPublication(ctx, &publication); err != nil {
		return ctrl.Result{RequeueAfter: databaseReconciliationInterval}, markAsFailed(ctx, r.Client, &publication, err)
	}

	return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, markAsReady(ctx, r.Client, &publication)
}

func (r *PublicationReconciler) reconcileFinalizer(ctx context.Context, publication apiv1.Publication) error {
	// add finalizer in non-deleted publications if not present
	if publication.DeletionTimestamp.IsZero() {
		if !controllerutil.AddFinalizer(&publication, utils.PublicationFinalizerName) {
			return nil
		}
		return r.Update(ctx, &publication)
	}

	// the publication is being deleted but no finalizer is present, we can quit
	if !controllerutil.ContainsFinalizer(&publication, utils.PublicationFinalizerName) {
		return nil
	}

	// This publication is being deleted but we still have work to do
	if publication.Spec.ReclaimPolicy == apiv1.PublicationReclaimDelete {
		if err := r.dropPublication(ctx, &publication); err != nil {
			return err
		}
	}

	// remove our finalizer from the list and update it.
	controllerutil.RemoveFinalizer(&publication, utils.PublicationFinalizerName)
	return r.Update(ctx, &publication)
}

// NewPublicationReconciler creates a new publication reconciler
func NewPublicationReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *PublicationReconciler {
	return &PublicationReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PublicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Publication{}).
		Named("instance-publication").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *PublicationReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
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
