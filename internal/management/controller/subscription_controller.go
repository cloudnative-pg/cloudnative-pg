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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// SubscriptionReconciler reconciles a Subscription object
type SubscriptionReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance                *postgres.Instance
	finalizerReconciler     *finalizerReconciler[*apiv1.Subscription]
	getDB                   func(name string) (*sql.DB, error)
	getPostgresMajorVersion func() (int, error)
}

// subscriptionReconciliationInterval is the time between the
// subscription reconciliation loop failures
const subscriptionReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=subscriptions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=subscriptions/status,verbs=get;update;patch

// Reconcile is the subscription reconciliation loop
func (r *SubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).
		WithName("subscription_reconciler").
		WithValues("subscriptionName", req.Name)

	// Get the subscription object
	var subscription apiv1.Subscription
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &subscription); err != nil {
		contextLogger.Trace("Could not fetch Subscription", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// This is not for me!
	if subscription.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		contextLogger.Trace("Subscription is not for this cluster",
			"cluster", subscription.Spec.ClusterRef.Name,
			"expected", r.instance.GetClusterName(),
		)
		return ctrl.Result{}, nil
	}

	// If everything is reconciled, we're done here
	if subscription.Generation == subscription.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		return ctrl.Result{}, markAsFailed(ctx, r.Client, &subscription, fmt.Errorf("while fetching the cluster: %w", err))
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	contextLogger.Info("Reconciling subscription")
	defer func() {
		contextLogger.Info("Reconciliation loop of subscription exited")
	}()

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		if err := markAsUnknown(ctx, r.Client, &subscription, errClusterIsReplica); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	if err := r.finalizerReconciler.reconcile(ctx, &subscription); err != nil {
		return ctrl.Result{}, fmt.Errorf("while reconciling the finalizer: %w", err)
	}
	if !subscription.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	// Let's get the connection string
	connString, err := getSubscriptionConnectionString(
		cluster,
		subscription.Spec.ExternalClusterName,
		subscription.Spec.PublicationDBName,
	)
	if err != nil {
		if markErr := markAsFailed(ctx, r.Client, &subscription, err); markErr != nil {
			contextLogger.Error(err, "while marking as failed the subscription resource",
				"error", err,
				"markError", markErr,
			)
			return ctrl.Result{}, fmt.Errorf(
				"encountered an error while marking as failed the subscription resource: %w, original error: %w",
				markErr,
				err)
		}
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	if res, err := detectConflictingManagers(ctx, r.Client, &subscription, &apiv1.SubscriptionList{}); err != nil ||
		!res.IsZero() {
		return res, err
	}

	if err := r.alignSubscription(ctx, &subscription, connString); err != nil {
		contextLogger.Error(err, "while reconciling subscription")
		if markErr := markAsFailed(ctx, r.Client, &subscription, err); markErr != nil {
			contextLogger.Error(err, "while marking as failed the subscription resource",
				"error", err,
				"markError", markErr,
			)
			return ctrl.Result{}, fmt.Errorf(
				"encountered an error while marking as failed the subscription resource: %w, original error: %w",
				markErr,
				err)
		}
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	contextLogger.Info("Reconciliation of subscription completed")
	if err := markAsReady(ctx, r.Client, &subscription); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
}

func (r *SubscriptionReconciler) evaluateDropSubscription(ctx context.Context, sub *apiv1.Subscription) error {
	if sub.Spec.ReclaimPolicy != apiv1.SubscriptionReclaimDelete {
		return nil
	}

	db, err := r.getDB(sub.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}
	return executeDropSubscription(ctx, db, sub.Spec.Name)
}

// NewSubscriptionReconciler creates a new subscription reconciler
func NewSubscriptionReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *SubscriptionReconciler {
	sr := &SubscriptionReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
		getDB: func(name string) (*sql.DB, error) {
			return instance.ConnectionPool().Connection(name)
		},
		getPostgresMajorVersion: func() (int, error) {
			version, err := instance.GetPgVersion()
			return int(version.Major), err //nolint:gosec
		},
	}
	sr.finalizerReconciler = newFinalizerReconciler(
		mgr.GetClient(),
		utils.SubscriptionFinalizerName,
		sr.evaluateDropSubscription,
	)

	return sr
}

// SetupWithManager sets up the controller with the Manager
func (r *SubscriptionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Subscription{}).
		Named("instance-subscription").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *SubscriptionReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	return getClusterFromInstance(ctx, r.Client, r.instance)
}

// getSubscriptionConnectionString gets the connection string to be used to connect to
// the specified external cluster, while connected to a pod of the specified cluster
func getSubscriptionConnectionString(
	cluster *apiv1.Cluster,
	externalClusterName string,
	databaseName string,
) (string, error) {
	externalCluster, ok := cluster.ExternalCluster(externalClusterName)
	if !ok {
		return "", fmt.Errorf("externalCluster '%s' not declared in cluster %s", externalClusterName, cluster.Name)
	}

	return external.GetServerConnectionString(&externalCluster, databaseName), nil
}
