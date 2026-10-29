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
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// crossNamespaceRBACReconciler maintains the RBAC objects granting the
// instance manager access to the resources of other namespaces.
//
// It runs apart from the reconciliation loop of the cluster: those objects
// live partly in the namespaces of the tenants, so a failure there, such as a
// namespace being terminated, must neither stop the management of the
// instances of a cluster shared by every tenant nor change its phase. Its
// failures are retried with their own backoff and reported as events.
type crossNamespaceRBACReconciler struct {
	*ClusterReconciler
}

// Reconcile creates, repairs and removes the cross-namespace RBAC objects of
// a cluster. The objects of a deleted cluster are removed while its deletion
// is reconciled by the cluster controller, and by the sweep.
func (r crossNamespaceRBACReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster apiv1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cluster.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	// The objects of a cluster not enabling the feature were left by a
	// previous cluster having the same name, or the setting was removed
	// bypassing the webhook: they must not keep granting access to every
	// namespace
	if !cluster.Spec.EnableCrossNamespaceResources {
		return ctrl.Result{}, r.deleteCrossNamespaceRBACByKey(ctx, req.NamespacedName, &cluster)
	}

	// Nothing changes until the operator is granted the access and restarted,
	// so this is reported without retrying
	if r.clusterRBACForbidden {
		r.Recorder.Event(&cluster, "Warning", "CrossNamespaceRBACForbidden",
			"Cannot grant the instances access to the resources of other namespaces: the operator "+
				"is not allowed to list and watch ClusterRoles and ClusterRoleBindings. "+
				"Update the RBAC of the operator and restart it")
		return ctrl.Result{}, nil
	}

	if err := r.reconcileCrossNamespaceRBAC(ctx, &cluster); err != nil {
		r.Recorder.Event(&cluster, "Warning", "CrossNamespaceRBACFailed",
			fmt.Sprintf("Cannot reconcile the RBAC objects for cross-namespace resources: %s", err))
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setupCrossNamespaceRBACController registers the crossNamespaceRBACReconciler
func (r *ClusterReconciler) setupCrossNamespaceRBACController(mgr ctrl.Manager) error {
	enabledPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		cluster, ok := obj.(*apiv1.Cluster)
		return ok && cluster.Spec.EnableCrossNamespaceResources
	})

	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		Named("cluster-cross-namespace-rbac").
		For(&apiv1.Cluster{}, builder.WithPredicates(
			enabledPredicate,
			predicate.GenerationChangedPredicate{},
		)).
		// The password Secrets of the roles decide which tenant namespaces
		// get a Role, and their deletion may leave one without any
		Watches(
			&apiv1.DatabaseRole{},
			handler.EnqueueRequestsFromMapFunc(mapClusterOwnedResourceToCluster),
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				isBeingDeletedPredicate,
			)),
		).
		// A change made to the RBAC objects by someone else is reverted
		Watches(
			&rbacv1.Role{},
			handler.EnqueueRequestsFromMapFunc(mapCrossNamespaceRBACToCluster),
		).
		Watches(
			&rbacv1.RoleBinding{},
			handler.EnqueueRequestsFromMapFunc(mapCrossNamespaceRBACToCluster),
		)

	if !r.clusterRBACForbidden {
		ctrlBuilder = ctrlBuilder.
			Watches(
				&rbacv1.ClusterRole{},
				handler.EnqueueRequestsFromMapFunc(mapCrossNamespaceRBACToCluster),
			).
			Watches(
				&rbacv1.ClusterRoleBinding{},
				handler.EnqueueRequestsFromMapFunc(mapCrossNamespaceRBACToCluster),
			)
	}

	return ctrlBuilder.Complete(crossNamespaceRBACReconciler{ClusterReconciler: r})
}

// mapCrossNamespaceRBACToCluster enqueues the cluster an RBAC object was
// created for, as recorded in its labels
func mapCrossNamespaceRBACToCluster(_ context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels[utils.KubernetesAppManagedByLabelName] != utils.ManagerName {
		return nil
	}

	key := types.NamespacedName{
		Namespace: labels[utils.ClusterNamespaceLabelName],
		Name:      labels[utils.ClusterLabelName],
	}
	if key.Namespace == "" || key.Name == "" {
		return nil
	}

	return []reconcile.Request{{NamespacedName: key}}
}
