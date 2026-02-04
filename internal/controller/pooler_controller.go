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
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	appsv1 "k8s.io/api/apps/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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

	// Early exit if some required prerequisite resources are not yet available
	if res := r.waitForPrerequisites(ctx, &pooler, resources); res != nil {
		return *res, nil
	}

	// Reconcile automatic pause/resume during switchover
	if resources.Cluster != nil {
		if err := r.reconcileSwitchoverPause(ctx, &pooler, resources.Cluster); err != nil {
			contextLogger.Error(err, "while reconciling switchover pause")
		}
	}

	if res := r.ensureManagedResourcesAreOwned(ctx, pooler, resources); !res.IsZero() {
		return res, nil
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
func (r *PoolerReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		For(&apiv1.Pooler{}).
		Named("pooler").
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToPooler()),
			builder.WithPredicates(secretsPoolerPredicate),
		).
		Watches(
			&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToPoolers()),
			builder.WithPredicates(clusterSwitchoverPredicate),
		).
		Complete(r)
}

// isOwnedByPoolerKind checks that an object is owned by a pooler and returns
// the owner name
func isOwnedByPoolerKind(obj client.Object) (string, bool) {
	owner := metav1.GetControllerOf(obj)
	if owner == nil {
		return "", false
	}

	if owner.Kind != apiv1.PoolerKind {
		return "", false
	}

	if owner.APIVersion != apiSGVString {
		return "", false
	}

	return owner.Name, true
}

func isOwnedByPooler(poolerName string, obj client.Object) bool {
	ownerName, isOwned := isOwnedByPoolerKind(obj)
	return isOwned && poolerName == ownerName
}

func (r *PoolerReconciler) ensureManagedResourcesAreOwned(
	ctx context.Context,
	pooler apiv1.Pooler,
	resources *poolerManagedResources,
) ctrl.Result {
	contextLogger := log.FromContext(ctx)

	var invalidData []interface{}
	if resources.Deployment != nil && !isOwnedByPooler(pooler.Name, resources.Deployment) {
		invalidData = append(invalidData, "notOwnedDeploymentName", resources.Deployment.Name)
	}

	if resources.Service != nil && !isOwnedByPooler(pooler.Name, resources.Service) {
		invalidData = append(invalidData, "notOwnedServiceName", resources.Service.Name)
	}

	if resources.Role != nil && !isOwnedByPooler(pooler.Name, resources.Role) {
		invalidData = append(invalidData, "notOwnedRoleName", resources.Role.Name)
	}

	if resources.RoleBinding != nil && !isOwnedByPooler(pooler.Name, resources.RoleBinding) {
		invalidData = append(invalidData, "notOwnedRoleBindingName", resources.RoleBinding.Name)
	}

	if len(invalidData) == 0 {
		return ctrl.Result{}
	}

	contextLogger.Error(
		errors.New("invalid ownership for managed resources"),
		"while ensuring managed resources are owned, requeueing...",
		invalidData...,
	)
	r.Recorder.Event(&pooler,
		"Warning",
		"InvalidOwnership",
		"found invalid ownership for managed resources, check logs")

	return ctrl.Result{RequeueAfter: 120 * time.Second}
}

// waitForPrerequisites centralizes the early-return checks for missing dependent resources
// to keep Reconcile lean. It logs a concise message and instructs the controller to
// requeue after a short delay when something is missing.
// Returns a non-nil *ctrl.Result when it requested a requeue; otherwise nil.
func (r *PoolerReconciler) waitForPrerequisites(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) *ctrl.Result {
	contextLogger := log.FromContext(ctx)
	waitResult := &ctrl.Result{RequeueAfter: 30 * time.Second}

	if resources.Cluster == nil {
		contextLogger.Info("Cluster not found, will retry in 30 seconds",
			"cluster", pooler.Spec.Cluster.Name)
		return waitResult
	}

	// For automated integration, we need AuthUserSecret
	if pooler.IsAutomatedIntegration() && resources.AuthUserSecret == nil {
		contextLogger.Info("AuthUserSecret not found, waiting 30 seconds",
			"secret", pooler.GetAuthQuerySecretName())
		return waitResult
	}

	// For manual TLS authentication to PostgreSQL, we need ServerTLSSecret
	if pooler.GetServerTLSSecretName() != "" && resources.ServerTLSSecret == nil {
		contextLogger.Info("ServerTLSSecret not found, waiting 30 seconds",
			"secret", pooler.GetServerTLSSecretName())
		return waitResult
	}

	// Always required: TLS certificates for accepting client connections
	if resources.ClientTLSSecret == nil {
		contextLogger.Info(
			"ClientTLSSecret not found, waiting 30 seconds",
			"secret", pooler.GetClientTLSSecretNameOrDefault(resources.Cluster))
		return waitResult
	}

	if resources.ClientCASecret == nil {
		contextLogger.Info(
			"ClientCASecret not found, waiting 30 seconds",
			"secret", pooler.GetClientCASecretNameOrDefault(resources.Cluster))
		return waitResult
	}

	if resources.ServerCASecret == nil {
		contextLogger.Info(
			"ServerCASecret not found, waiting 30 seconds",
			"secret", pooler.GetServerCASecretNameOrDefault(resources.Cluster))
		return waitResult
	}

	return nil
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

		return result
	}
}

// mapClusterToPoolers returns a function mapping cluster events to the poolers referencing them
func (r *PoolerReconciler) mapClusterToPoolers() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) (result []reconcile.Request) {
		cluster, ok := obj.(*apiv1.Cluster)
		if !ok {
			return nil
		}

		var poolers apiv1.PoolerList
		if err := r.List(ctx, &poolers,
			client.InNamespace(cluster.Namespace),
		); err != nil {
			log.FromContext(ctx).Error(err, "while getting pooler list for cluster",
				"namespace", cluster.Namespace, "cluster", cluster.Name)
			return nil
		}

		for idx := range poolers.Items {
			if poolers.Items[idx].Spec.Cluster.Name == cluster.Name {
				result = append(result, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      poolers.Items[idx].Name,
						Namespace: poolers.Items[idx].Namespace,
					},
				})
			}
		}

		return result
	}
}

// getPoolersUsingSecret get a list of poolers which are using the passed secret
func getPoolersUsingSecret(poolers apiv1.PoolerList, secret *corev1.Secret) (requests []types.NamespacedName) {
	for _, pooler := range poolers.Items {
		if name, ok := isOwnedByPoolerKind(secret); ok && pooler.Name == name {
			requests = append(requests,
				types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				})
			continue
		}

		if pooler.Spec.PgBouncer != nil && pooler.GetAuthQuerySecretName() == secret.Name {
			requests = append(requests,
				types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				},
			)
			continue
		}

		if pooler.GetServerTLSSecretName() == secret.Name {
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
