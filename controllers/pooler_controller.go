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

package controllers

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// PoolerReconciler reconciles a Pooler object
type PoolerReconciler struct {
	client.Client
	DiscoveryClient discovery.DiscoveryInterface
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get;update;patch;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;create;delete;update;patch;list;watch
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;create;delete;update;patch;list;watch

// Reconcile implements the main reconciliation loop for pooler objects
func (r *PoolerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	var pooler apiv1.Pooler
	if err := r.Get(ctx, req.NamespacedName, &pooler); err != nil {
		// This also happens when you delete a Pooler resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			contextLogger.Info("Resource has been deleted")
			return ctrl.Result{}, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return ctrl.Result{}, fmt.Errorf("cannot get the pooler resource: %w", err)
	}

	// We make sure that there isn't a cluster with the same name as the pooler
	conflictingCluster, err := getClusterOrNil(ctx, r.Client, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while getting cluster resource: %w", err)
	}

	if conflictingCluster != nil {
		r.Recorder.Event(
			&pooler,
			"Warning",
			"NameClash",
			"Name clash between Pooler and Cluster detected, resource reconciliation skipped")
		return ctrl.Result{}, nil
	}

	// Get the set of resources we directly manage and their status
	resources, err := r.getManagedResources(ctx, &pooler)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while getting managed resources: %w", err)
	}

	if resources.Cluster == nil {
		contextLogger.Info("Cluster not found, will retry in 30 seconds", "cluster", pooler.Spec.Cluster.Name)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if resources.AuthUserSecret == nil {
		contextLogger.Info("AuthUserSecret not found, waiting 30 seconds", "secret", pooler.GetAuthQuerySecretName())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Update the status of the Pooler resource given what we read
	// from the controlled resources
	if err := r.updatePoolerStatus(ctx, &pooler, resources); err != nil {
		if apierrs.IsConflict(err) {
			// Requeue a reconciliation loop since the resource
			// changed while we were synchronizing it
			contextLogger.Debug("Conflict while reconciling pooler status", "error", err)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Take the required actions to align the spec with the collected status
	return ctrl.Result{}, r.updateOwnedObjects(ctx, &pooler, resources)
}

// SetupWithManager setup this controller inside the controller manager
func (r *PoolerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Pooler{}).
		Owns(&v1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToPooler()),
			builder.WithPredicates(secretsPoolerPredicate),
		).
		Complete(r)
}

// isOwnedByPooler checks that an object is owned by a pooler and returns
// the owner name
func isOwnedByPooler(obj client.Object) (string, bool) {
	owner := metav1.GetControllerOf(obj)
	if owner == nil {
		return "", false
	}

	if owner.Kind != apiv1.PoolerKind {
		return "", false
	}

	if owner.APIVersion != apiGVString {
		return "", false
	}

	return owner.Name, true
}

// mapSecretToPooler returns a function mapping secrets events to the poolers using them
func (r *PoolerReconciler) mapSecretToPooler() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) (result []reconcile.Request) {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		var poolers apiv1.PoolerList

		// get all the clusters handled by the operator in the configmap namespace
		if err := r.List(ctx, &poolers,
			client.InNamespace(secret.Namespace),
		); err != nil {
			log.FromContext(ctx).Error(err, "while getting pooler list for secret",
				"namespace", secret.Namespace, "secret", secret.Name)
			return nil
		}

		// filter the cluster list preserving only the ones which are using
		// the passed secret
		filteredPoolersList := getPoolersUsingSecret(poolers, secret)
		result = make([]reconcile.Request, len(filteredPoolersList))
		for idx, value := range filteredPoolersList {
			result[idx] = reconcile.Request{NamespacedName: value}
		}

		return
	}
}

// getPoolersUsingSecret get a list of poolers which are using the passed secret
func getPoolersUsingSecret(poolers apiv1.PoolerList, secret *corev1.Secret) (requests []types.NamespacedName) {
	for _, pooler := range poolers.Items {
		if pooler.Spec.PgBouncer != nil && pooler.GetAuthQuerySecretName() == secret.Name {
			requests = append(requests,
				types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				},
			)
			continue
		}
	}
	return requests
}
