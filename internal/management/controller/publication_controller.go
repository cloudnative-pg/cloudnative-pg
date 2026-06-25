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

package controller

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PublicationReconciler reconciles a Publication object
type PublicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance            *postgres.Instance
	finalizerReconciler *finalizerReconciler[*apiv1.Publication]
	getDB               func(name string) (*sql.DB, error)
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
	contextLogger := log.FromContext(ctx).
		WithName("publication_reconciler").
		WithValues("publicationName", req.Name)

	// Get the publication object
	var publication apiv1.Publication
	if err := r.Get(ctx, client.ObjectKey{
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
		// A reconciled publication keeps its status: the cluster may be
		// gone or unreadable while it is being deleted.
		if publication.Generation == publication.Status.ObservedGeneration {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, markAsFailed(ctx, r.Client, &publication, fmt.Errorf("while fetching the cluster: %w", err))
	}

	// If everything is reconciled, we're done here
	if publication.Generation == publication.Status.ObservedGeneration {
		// ...unless the cluster moved in or out of the replica role after
		// the publication was applied: report the demotion on the status,
		// and evaluate the publication again after the promotion.
		result, proceed, err := handleReplicaRoleTransition(
			ctx, r.Client, r.instance, cluster, &publication, publicationReconciliationInterval)
		if err != nil || !proceed {
			return result, err
		}
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: publicationReconciliationInterval}, nil
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return ctrl.Result{RequeueAfter: publicationReconciliationInterval}, nil
	}

	contextLogger.Info("Reconciling publication")
	defer func() {
		contextLogger.Info("Reconciliation loop of publication exited")
	}()

	// A replica cluster is read-only, so the apply path is gated here. Deletion
	// is still allowed through: an object that acquired its finalizer while this
	// cluster was primary must release it after a demotion. The drop itself is
	// skipped on a replica (see evaluateDropPublication).
	if cluster.IsReplica() && publication.GetDeletionTimestamp().IsZero() {
		if err := markAsUnknown(ctx, r.Client, &publication, errClusterIsReplica); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: publicationReconciliationInterval}, nil
	}

	// The detection only gates the apply path: a publication being deleted
	// must release its finalizer regardless of conflicting managers. It stays
	// ahead of the finalizer reconciler so that a conflicting publication
	// never acquires the finalizer.
	if publication.GetDeletionTimestamp().IsZero() {
		if res, err := detectConflictingManagers(ctx, r.Client, &publication, &apiv1.PublicationList{}); err != nil ||
			!res.IsZero() {
			return res, err
		}
	}

	if err := r.finalizerReconciler.reconcile(ctx, &publication); err != nil {
		return ctrl.Result{}, fmt.Errorf("while reconciling the finalizer: %w", err)
	}
	if !publication.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	if err := r.alignPublication(ctx, &publication); err != nil {
		contextLogger.Error(err, "while reconciling publication")
		if markErr := markAsFailed(ctx, r.Client, &publication, err); markErr != nil {
			contextLogger.Error(err, "while marking as failed the publication resource",
				"error", err,
				"markError", markErr,
			)
			return ctrl.Result{}, fmt.Errorf(
				"encountered an error while marking as failed the publication resource: %w, original error: %w",
				markErr,
				err)
		}
		return ctrl.Result{RequeueAfter: publicationReconciliationInterval}, nil
	}

	contextLogger.Info("Reconciliation of publication completed")
	if err := markAsReady(ctx, r.Client, &publication); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: publicationReconciliationInterval}, nil
}

func (r *PublicationReconciler) evaluateDropPublication(ctx context.Context, pub *apiv1.Publication) error {
	if pub.Spec.ReclaimPolicy != apiv1.PublicationReclaimDelete {
		return nil
	}

	// On a replica we cannot drop the publication: return without touching
	// PostgreSQL so the finalizer is released. Dropping it is left to the
	// primary cluster's own Publication object, if any.
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		return fmt.Errorf("while fetching the cluster: %w", err)
	}
	if cluster.IsReplica() {
		return nil
	}

	// An object that never reconciled does not own the publication: a
	// conflicting duplicate is blocked before applying anything, and its
	// deletion must not drop the publication owned by the surviving object.
	if !pub.HasReconciliations() {
		return nil
	}

	db, err := r.getDB(pub.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	return executeDropPublication(ctx, db, pub.Spec.Name)
}

// NewPublicationReconciler creates a new publication reconciler
func NewPublicationReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *PublicationReconciler {
	pr := &PublicationReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
		getDB: func(name string) (*sql.DB, error) {
			return instance.ConnectionPool().Connection(name)
		},
	}

	pr.finalizerReconciler = newFinalizerReconciler(
		mgr.GetClient(),
		utils.PublicationFinalizerName,
		pr.evaluateDropPublication,
	)

	return pr
}

// SetupWithManager sets up the controller with the Manager.
func (r *PublicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Publication{}).
		Watches(
			&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(mapClusterToManagedResources(
				r.instance, mgr.GetClient(),
				func() client.ObjectList { return &apiv1.PublicationList{} })),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("instance-publication").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *PublicationReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	return getClusterFromInstance(ctx, r.Client, r.instance)
}
