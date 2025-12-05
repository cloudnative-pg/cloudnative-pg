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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

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

	// If everything is reconciled, we're done here
	if publication.Generation == publication.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		return ctrl.Result{}, markAsFailed(ctx, r.Client, &publication, fmt.Errorf("while fetching the cluster: %w", err))
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

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		if err := markAsUnknown(ctx, r.Client, &publication, errClusterIsReplica); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: publicationReconciliationInterval}, nil
	}

	if res, err := detectConflictingManagers(ctx, r.Client, &publication, &apiv1.PublicationList{}); err != nil ||
		!res.IsZero() {
		return res, err
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
		Named("instance-publication").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *PublicationReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	return getClusterFromInstance(ctx, r.Client, r.instance)
}
