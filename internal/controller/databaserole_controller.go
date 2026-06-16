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
	"reflect"
	"time"

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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

// DatabaseRoleReconciler reconciles a DatabaseRole object
type DatabaseRoleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// clientCertReconcileInterval is the requeue period for roles with client
// certificate issuance enabled, ensuring the certificate is renewed before
// expiry even in the absence of a triggering event.
const clientCertReconcileInterval = time.Hour

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databaseroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databaseroles/status,verbs=get;update;patch;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the main reconciliation loop for Role objects
func (r *DatabaseRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	var role apiv1.DatabaseRole
	if err := r.Get(ctx, req.NamespacedName, &role); err != nil {
		if apierrs.IsNotFound(err) {
			contextLogger.Info("Resource has been deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("cannot get the role resource: %w", err)
	}

	if err := r.reconcilePasswordCondition(ctx, &role); err != nil {
		return ctrl.Result{}, err
	}

	origRole := role.DeepCopy()

	if err := r.reconcileClientCertificate(ctx, &role); err != nil {
		return ctrl.Result{}, err
	}

	// A DatabaseRole has two status writers: the instance manager owns every
	// field except the PasswordSecretChange condition (handled above) and the
	// ClientCertificate state set here. Merge-patch so we only touch our own
	// fields and never clobber the instance manager's update.
	if !reflect.DeepEqual(origRole.Status, role.Status) {
		if err := r.Status().Patch(ctx, &role, client.MergeFrom(origRole)); err != nil {
			return ctrl.Result{}, fmt.Errorf("while patching role status: %w", err)
		}
	}

	if role.Spec.IssueClientCertificate {
		return ctrl.Result{RequeueAfter: clientCertReconcileInterval}, nil
	}
	return ctrl.Result{}, nil
}

// reconcilePasswordCondition manages the ConditionPasswordSecretChange status condition.
// If the role has no password secret, the condition is removed. If the secret is found,
// the condition is set with the secret's ResourceVersion so the instance manager can
// detect when the password changed.
func (r *DatabaseRoleReconciler) reconcilePasswordCondition(
	ctx context.Context,
	role *apiv1.DatabaseRole,
) error {
	if role.Spec.PasswordSecret == nil {
		// If passwordSecret was removed, clear any stale PasswordSecretChange
		// condition left over from a previously configured secret.
		if meta.FindStatusCondition(role.Status.Conditions, string(apiv1.ConditionPasswordSecretChange)) != nil {
			oldRole := role.DeepCopy()
			meta.RemoveStatusCondition(&role.Status.Conditions, string(apiv1.ConditionPasswordSecretChange))
			if err := r.Status().Patch(ctx, role, client.MergeFrom(oldRole)); err != nil {
				return fmt.Errorf("while clearing stale password condition: %w", err)
			}
		}
		return nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: role.Namespace,
		Name:      role.Spec.PasswordSecret.Name,
	}, &secret); err != nil {
		// There's no need to fill the operator log with errors
		// if the secret still doesn't exist.
		if apierrs.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf(
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
	oldRole := role.DeepCopy()
	changed := meta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
		Type:    string(apiv1.ConditionPasswordSecretChange),
		Status:  metav1.ConditionTrue,
		Reason:  "ChangeDetected",
		Message: secret.ResourceVersion,
	})
	if changed {
		if err := r.Status().Patch(ctx, role, client.MergeFrom(oldRole)); err != nil {
			return fmt.Errorf("while setting role status: %w", err)
		}
	}

	return nil
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

	// Only consider secrets that carry a CA certificate, to avoid triggering
	// a full role list on every unrelated secret change.
	caSecretPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return false
		}
		_, hasCA := secret.Data[certs.CACertKey]
		return hasCA
	})

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		For(&apiv1.DatabaseRole{}).
		Named("database-role").
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToRole()),
			builder.WithPredicates(rolesSecretsPredicate),
		).
		// Cert secrets owned by a DatabaseRole: re-enqueue the owner on any change
		// so that an externally deleted or modified cert is promptly regenerated.
		Owns(&corev1.Secret{}).
		// CA secrets: when the cluster's client CA rotates, re-issue certs for all
		// DatabaseRoles that reference that cluster.
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapClientCASecretToRoles()),
			builder.WithPredicates(caSecretPredicate),
		).
		// Cluster: when a cluster appears or its spec changes, (re)evaluate the
		// roles that reference it. The generation predicate skips the frequent
		// status-only updates that would otherwise churn every referencing role.
		Watches(
			&apiv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToRoles()),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Complete(r)
}

// mapSecretToRole returns a function mapping secret events to the roles using them
// as password secrets.
func (r *DatabaseRoleReconciler) mapSecretToRole() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) (result []reconcile.Request) {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		var roles apiv1.DatabaseRoleList
		if err := r.List(ctx, &roles, client.InNamespace(secret.Namespace)); err != nil {
			log.FromContext(ctx).Error(err, "while listing roles for secret",
				"namespace", secret.Namespace, "secret", secret.Name)
			return nil
		}

		filteredRoles := getRolesUsingSecret(roles, secret)
		result = make([]reconcile.Request, len(filteredRoles))
		for idx, value := range filteredRoles {
			result[idx] = reconcile.Request{NamespacedName: value}
		}
		return result
	}
}

// mapClientCASecretToRoles returns a function that enqueues all DatabaseRoles
// whose cluster uses the changed Secret as its client CA.
func (r *DatabaseRoleReconciler) mapClientCASecretToRoles() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		var clusters apiv1.ClusterList
		if err := r.List(ctx, &clusters, client.InNamespace(secret.Namespace)); err != nil {
			log.FromContext(ctx).Error(err, "while listing clusters for CA secret",
				"secret", secret.Name)
			return nil
		}

		// Collect the clusters whose client CA is the changed Secret.
		matchingClusters := make(map[string]struct{})
		for i := range clusters.Items {
			if clusters.Items[i].GetClientCASecretName() == secret.Name {
				matchingClusters[clusters.Items[i].Name] = struct{}{}
			}
		}
		if len(matchingClusters) == 0 {
			return nil
		}

		var roles apiv1.DatabaseRoleList
		if err := r.List(ctx, &roles, client.InNamespace(secret.Namespace)); err != nil {
			log.FromContext(ctx).Error(err, "while listing roles for CA secret", "secret", secret.Name)
			return nil
		}

		var result []reconcile.Request
		for i := range roles.Items {
			role := &roles.Items[i]
			if !role.Spec.IssueClientCertificate {
				continue
			}
			if _, ok := matchingClusters[role.Spec.ClusterRef.Name]; ok {
				result = append(result, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: role.Name, Namespace: role.Namespace},
				})
			}
		}
		return result
	}
}

// mapClusterToRoles returns a function that enqueues all DatabaseRoles
// referencing the changed Cluster.
func (r *DatabaseRoleReconciler) mapClusterToRoles() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cluster, ok := obj.(*apiv1.Cluster)
		if !ok {
			return nil
		}

		var roles apiv1.DatabaseRoleList
		if err := r.List(ctx, &roles, client.InNamespace(cluster.Namespace)); err != nil {
			log.FromContext(ctx).Error(err, "while listing roles for cluster", "cluster", cluster.Name)
			return nil
		}

		var result []reconcile.Request
		for i := range roles.Items {
			role := &roles.Items[i]
			if role.Spec.ClusterRef.Name == cluster.Name {
				result = append(result, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: role.Name, Namespace: role.Namespace},
				})
			}
		}
		return result
	}
}

// getRolesUsingSecret returns the namespaced names of roles that reference the
// given secret as their password secret.
func getRolesUsingSecret(roles apiv1.DatabaseRoleList, secret *corev1.Secret) (requests []types.NamespacedName) {
	for i := range roles.Items {
		role := &roles.Items[i]
		if role.Spec.PasswordSecret != nil && role.Spec.PasswordSecret.Name == secret.Name {
			requests = append(requests,
				types.NamespacedName{
					Name:      role.Name,
					Namespace: role.Namespace,
				})
		}
	}
	return requests
}
