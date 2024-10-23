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

	instance            *postgres.Instance
	finalizerReconciler *finalizerReconciler[*apiv1.Subscription]
}

// subscriptionReconciliationInterval is the time between the
// subscription reconciliation loop failures
const subscriptionReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=subscriptions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=subscriptions/status,verbs=get;update;patch

// Reconcile is the subscription reconciliation loop
func (r *SubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the subscription object
	var subscription apiv1.Subscription
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &subscription); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the Cluster from the cache
	cluster, err := r.GetCluster(ctx)
	if err != nil {
		return ctrl.Result{}, markAsFailed(ctx, r.Client, &subscription, fmt.Errorf("while fetching the cluster: %w", err))
	}

	// This is not for me!
	if subscription.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return ctrl.Result{}, nil
	}

	// This is not for me, at least now
	if cluster.Status.CurrentPrimary != r.instance.GetPodName() {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	// Still not for me, we're waiting for a switchover
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, nil
	}

	// Cannot do anything on a replica cluster
	if cluster.IsReplica() {
		err := markAsFailed(ctx, r.Client, &subscription, errClusterIsReplica)
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, err
	}

	if err := r.finalizerReconciler.reconcile(ctx, &subscription); err != nil {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval},
			fmt.Errorf("while reconciling the finalizer: %w", err)
	}

	// If everything is reconciled, we're done here
	if subscription.Generation == subscription.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Let's get the connection string
	connString, err := getSubscriptionConnectionString(
		cluster,
		subscription.Spec.ExternalClusterName,
		"", // TODO: should we have a way to force dbname?
	)
	if err != nil {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, markAsFailed(
			ctx,
			r.Client,
			&subscription,
			err,
		)
	}

	if err := r.alignSubscription(
		ctx,
		&subscription,
		connString,
	); err != nil {
		return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, markAsFailed(
			ctx,
			r.Client,
			&subscription,
			err,
		)
	}

	return ctrl.Result{RequeueAfter: subscriptionReconciliationInterval}, markAsReady(
		ctx,
		r.Client,
		&subscription,
	)
}

// NewSubscriptionReconciler creates a new subscription reconciler
func NewSubscriptionReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *SubscriptionReconciler {
	onFinalizerDelete := func(ctx context.Context, sub *apiv1.Subscription) error {
		if sub.Spec.ReclaimPolicy == apiv1.SubscriptionReclaimDelete {
			return dropSubscription(ctx, instance, sub)
		}
		return nil
	}
	return &SubscriptionReconciler{
		Client:              mgr.GetClient(),
		instance:            instance,
		finalizerReconciler: newFinalizerReconciler(mgr.GetClient(), utils.SubscriptionFinalizerName, onFinalizerDelete),
	}
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
