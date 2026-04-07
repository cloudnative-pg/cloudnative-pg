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

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// DatabaseRoleReconciler reconciles a Role object
type DatabaseRoleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databaseroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databaseroles/status,verbs=get;update;patch;watch

// Reconcile implements the main reconciliation loop for Role objects
func (r *DatabaseRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	var role apiv1.DatabaseRole
	if err := r.Get(ctx, req.NamespacedName, &role); err != nil {
		// This also happens when you delete a Role resource in k8s. If
		// that's the case, let's just wait for the Kubernetes garbage collector
		// to remove all the Pods of the cluster.
		if apierrs.IsNotFound(err) {
			contextLogger.Info("Resource has been deleted")
			return ctrl.Result{}, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return ctrl.Result{}, fmt.Errorf("cannot get the role resource: %w", err)
	}

	if role.Spec.PasswordSecret == nil {
		return ctrl.Result{}, nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      role.Spec.PasswordSecret.Name,
	}, &secret); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"while getting secret %q referred by role %q: %w",
			role.Spec.PasswordSecret.Name,
			role.Name,
			err,
		)
	}

	// The instance manager, which runs the controller that applies the Role spec
	// to the PostgreSQL database, cannot watch Secrets directly. Granting that
	// permission would allow it to watch all Secrets in the namespace, which is
	// undesirable from a security standpoint. Unfortunately, the Kubernetes API
	// does not support watching a specific subset of Secrets.
	//
	// To work around this, the operator (which has the necessary permissions)
	// watches Secrets and detects changes to those referenced by Role objects.
	// When a change is detected, the operator updates a condition on the Role,
	// storing the Secret's ResourceVersion. This status update triggers a
	// reconciliation cycle in the instance manager, which then reads the updated
	// password and applies it to PostgreSQL.
	changed := meta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
		Type:    string(apiv1.ConditionPasswordSecretChange),
		Status:  metav1.ConditionTrue,
		Reason:  "ChangeDetected",
		Message: secret.ResourceVersion,
	})
	if changed {
		if err := r.Status().Update(ctx, &role); err != nil {
			return ctrl.Result{}, fmt.Errorf("while setting role status: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager setup this controller inside the controller manager
func (r *DatabaseRoleReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	rolesSecretsPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasReloadLabelSet(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasReloadLabelSet(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasReloadLabelSet(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasReloadLabelSet(e.ObjectNew)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		For(&apiv1.DatabaseRole{}).
		Named("role").
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToRole()),
			builder.WithPredicates(rolesSecretsPredicate),
		).
		Complete(r)
}

// mapSecretToRole returns a function mapping secrets events to the roles using them
func (r *DatabaseRoleReconciler) mapSecretToRole() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) (result []reconcile.Request) {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		// get all the clusters handled by the operator in the configmap namespace
		var roles apiv1.DatabaseRoleList
		if err := r.List(ctx, &roles,
			client.InNamespace(secret.Namespace),
		); err != nil {
			log.FromContext(ctx).Error(err, "while getting roles list for secret",
				"namespace", secret.Namespace, "secret", secret.Name)
			return nil
		}

		// filter the cluster list preserving only the ones which are using
		// the passed secret
		filteredRolesList := getRolesUsingSecret(roles, secret)
		result = make([]reconcile.Request, len(filteredRolesList))
		for idx, value := range filteredRolesList {
			result[idx] = reconcile.Request{NamespacedName: value}
		}

		return result
	}
}

// getRolesUsingSecret get a list of roles which are using the passed secret
func getRolesUsingSecret(roles apiv1.DatabaseRoleList, secret *corev1.Secret) (requests []types.NamespacedName) {
	for _, role := range roles.Items {
		if role.Spec.PasswordSecret != nil && role.Spec.PasswordSecret.Name == secret.Name {
			requests = append(requests,
				types.NamespacedName{
					Name:      role.Name,
					Namespace: role.Namespace,
				})
			continue
		}
	}
	return requests
}
