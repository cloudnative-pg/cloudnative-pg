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

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// SubscriptionReconciler reconciles a Subscription object
type SubscriptionReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instance *postgres.Instance
}

// subscriptionReconciliationInterval is the time between the
// subscription reconciliation loop failures
const subscriptionReconciliationInterval = 30 * time.Second

// subscriptionFinalizerName is the name of the finalizer
// triggering the deletion of the subscription
const subscriptionFinalizerName = utils.MetadataNamespace + "/deleteSubscription"

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
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	// This is not for me!
	if subscription.Spec.ClusterRef.Name != r.instance.GetClusterName() {
		return ctrl.Result{}, nil
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
			&subscription,
			errClusterIsReplica,
		)
	}

	// Add the finalizer if we don't have it
	// nolint:nestif
	if subscription.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&subscription, subscriptionFinalizerName) {
			if err := r.Update(ctx, &subscription); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// This subscription is being deleted
		if controllerutil.ContainsFinalizer(&subscription, subscriptionFinalizerName) {
			if subscription.Spec.ReclaimPolicy == apiv1.SubscriptionReclaimDelete {
				if err := r.dropSubscription(ctx, &subscription); err != nil {
					return ctrl.Result{}, err
				}
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&subscription, subscriptionFinalizerName)
			if err := r.Update(ctx, &subscription); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}
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
		return r.failedReconciliation(
			ctx,
			&subscription,
			err,
		)
	}

	if err := r.alignSubscription(
		ctx,
		&subscription,
		connString,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&subscription,
			err,
		)
	}

	return r.succeededReconciliation(
		ctx,
		&subscription,
	)
}

// NewSubscriptionReconciler creates a new subscription reconciler
func NewSubscriptionReconciler(
	mgr manager.Manager,
	instance *postgres.Instance,
) *SubscriptionReconciler {
	return &SubscriptionReconciler{
		Client:   mgr.GetClient(),
		instance: instance,
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *SubscriptionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Subscription{}).
		Complete(r)
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *SubscriptionReconciler) failedReconciliation(
	ctx context.Context,
	subscription *apiv1.Subscription,
	err error,
) (ctrl.Result, error) {
	oldSubscription := subscription.DeepCopy()
	subscription.Status.Error = err.Error()
	subscription.Status.Ready = false

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contain the human-readable error
		subscription.Status.Error = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, subscription, client.MergeFrom(oldSubscription)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *SubscriptionReconciler) succeededReconciliation(
	ctx context.Context,
	subscription *apiv1.Subscription,
) (ctrl.Result, error) {
	oldSubscription := subscription.DeepCopy()
	subscription.Status.Error = ""
	subscription.Status.Ready = true
	subscription.Status.ObservedGeneration = subscription.Generation

	if err := r.Client.Status().Patch(ctx, subscription, client.MergeFrom(oldSubscription)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: subscriptionReconciliationInterval,
	}, nil
}

// GetCluster gets the managed cluster through the client
func (r *SubscriptionReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
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

// getSubscriptionConnectionString gets the connection string to be used to connect to
// the specified external cluster, while connected to a pod of the specified cluster
func getSubscriptionConnectionString(
	cluster *apiv1.Cluster,
	externalClusterName string,
	databaseName string,
) (string, error) {
	externalCluster, ok := cluster.ExternalCluster(externalClusterName)
	if !ok {
		return "", fmt.Errorf("external cluster %s not found in the cluster %s", externalClusterName, cluster.Name)
	}

	return external.GetServerConnectionString(&externalCluster, databaseName), nil
}
