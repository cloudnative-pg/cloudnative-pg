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

	"github.com/cloudnative-pg/machinery/pkg/log"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/clusterindex"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// errCrossNamespaceRBACNotOwned is returned when an RBAC object having the
// name computed for a cluster exists but was not created for it
var errCrossNamespaceRBACNotOwned = errors.New("exists and is not managed for this cluster")

// reconcileCrossNamespaceRBAC creates and repairs the ClusterRole and the
// ClusterRoleBinding granting the instance manager access to the resources
// referring to its cluster from other namespaces.
//
// Nothing is read when the feature is disabled: the setting cannot be
// disabled once enabled, so there is nothing to clean up, and reading the
// cluster-scoped RBAC objects of every cluster would be wasted API calls.
func (r *ClusterReconciler) reconcileCrossNamespaceRBAC(ctx context.Context, cluster *apiv1.Cluster) error {
	if !cluster.Spec.EnableCrossNamespaceResources {
		return nil
	}

	if err := r.reconcileCrossNamespaceClusterRole(ctx, cluster); err != nil {
		return err
	}

	if err := r.reconcileCrossNamespaceClusterRoleBinding(ctx, cluster); err != nil {
		return err
	}

	return r.reconcileCrossNamespaceSecretsRBAC(ctx, cluster)
}

// reconcileCrossNamespaceClusterRole creates the cross-namespace ClusterRole
// of the cluster, or restores its rules
func (r *ClusterReconciler) reconcileCrossNamespaceClusterRole(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
	desired := specs.CreateCrossNamespaceRole(*cluster)

	var existing rbacv1.ClusterRole
	if err := r.Get(ctx, client.ObjectKey{Name: desired.Name}, &existing); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting ClusterRole: %w", err)
		}

		contextLogger.Info("Creating ClusterRole for cross-namespace resources access", "name", desired.Name)
		r.Recorder.Event(cluster, "Normal", "CreatingClusterRole",
			fmt.Sprintf("Creating ClusterRole %s for cross-namespace resources access", desired.Name))
		if err := r.Create(ctx, &desired); err != nil {
			return fmt.Errorf("while creating ClusterRole: %w", err)
		}
		return nil
	}

	if !specs.IsCrossNamespaceRBACOwnedBy(existing.Labels, cluster.Namespace, cluster.Name) {
		return fmt.Errorf("ClusterRole %s %w", desired.Name, errCrossNamespaceRBACNotOwned)
	}

	if equality.Semantic.DeepEqual(existing.Rules, desired.Rules) {
		return nil
	}

	contextLogger.Info("Updating ClusterRole for cross-namespace resources access", "name", desired.Name)
	patched := existing.DeepCopy()
	patched.Rules = desired.Rules
	if err := r.Patch(ctx, patched, client.MergeFrom(&existing)); err != nil {
		return fmt.Errorf("while patching ClusterRole: %w", err)
	}
	return nil
}

// reconcileCrossNamespaceClusterRoleBinding creates the cross-namespace
// ClusterRoleBinding of the cluster, or restores its subjects and role
func (r *ClusterReconciler) reconcileCrossNamespaceClusterRoleBinding(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	contextLogger := log.FromContext(ctx)
	desired := specs.CreateCrossNamespaceRoleBinding(*cluster)

	var existing rbacv1.ClusterRoleBinding
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name}, &existing)
	if err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("while getting ClusterRoleBinding: %w", err)
	}

	if err == nil {
		if !specs.IsCrossNamespaceRBACOwnedBy(existing.Labels, cluster.Namespace, cluster.Name) {
			return fmt.Errorf("ClusterRoleBinding %s %w", desired.Name, errCrossNamespaceRBACNotOwned)
		}

		// The role of a binding cannot be changed: a binding pointing to
		// another role is replaced
		if existing.RoleRef == desired.RoleRef {
			return r.patchCrossNamespaceClusterRoleBindingSubjects(ctx, &existing, desired.Subjects)
		}

		contextLogger.Info("Replacing ClusterRoleBinding for cross-namespace resources access",
			"name", desired.Name, "roleRef", existing.RoleRef)
		if err := r.Delete(ctx, &existing); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("while deleting ClusterRoleBinding: %w", err)
		}
	}

	contextLogger.Info("Creating ClusterRoleBinding for cross-namespace resources access", "name", desired.Name)
	r.Recorder.Event(cluster, "Normal", "CreatingClusterRoleBinding",
		fmt.Sprintf("Creating ClusterRoleBinding %s for cross-namespace resources access", desired.Name))
	if err := r.Create(ctx, &desired); err != nil {
		return fmt.Errorf("while creating ClusterRoleBinding: %w", err)
	}
	return nil
}

// patchCrossNamespaceClusterRoleBindingSubjects restores the subjects of the
// cross-namespace ClusterRoleBinding of a cluster, which must only grant its
// role to the ServiceAccount of the cluster
func (r *ClusterReconciler) patchCrossNamespaceClusterRoleBindingSubjects(
	ctx context.Context,
	existing *rbacv1.ClusterRoleBinding,
	subjects []rbacv1.Subject,
) error {
	if equality.Semantic.DeepEqual(existing.Subjects, subjects) {
		return nil
	}

	log.FromContext(ctx).Info("Updating ClusterRoleBinding for cross-namespace resources access",
		"name", existing.Name)
	patched := existing.DeepCopy()
	patched.Subjects = subjects
	if err := r.Patch(ctx, patched, client.MergeFrom(existing)); err != nil {
		return fmt.Errorf("while patching ClusterRoleBinding: %w", err)
	}
	return nil
}

// deleteCrossNamespaceRBACByKey deletes the ClusterRole and the
// ClusterRoleBinding created for cross-namespace resources access by the
// cluster having the passed namespaced name.
//
// Those resources are cluster-scoped, so they cannot be garbage collected
// through the namespaced owner reference of the Cluster and must be removed
// explicitly, including when the Cluster is being deleted. An object having
// the computed name is only deleted when its labels show that it was created
// for this cluster.
//
// The recorder object is optional, as the Cluster may already be gone by the
// time its deletion is reconciled: when nil, no event is emitted.
func (r *ClusterReconciler) deleteCrossNamespaceRBACByKey(
	ctx context.Context,
	key types.NamespacedName,
	recorderObject client.Object,
) error {
	name := specs.GetCrossNamespaceRoleNameFor(key.Namespace, key.Name)

	if err := r.deleteCrossNamespaceSecretsRBAC(ctx, key, nil); err != nil {
		return err
	}

	if err := r.deleteOwnedCrossNamespaceRBAC(
		ctx, key, recorderObject, &rbacv1.ClusterRoleBinding{}, name, "ClusterRoleBinding",
	); err != nil {
		return err
	}

	return r.deleteOwnedCrossNamespaceRBAC(
		ctx, key, recorderObject, &rbacv1.ClusterRole{}, name, "ClusterRole",
	)
}

// deleteOwnedCrossNamespaceRBAC deletes the cluster-scoped RBAC object having
// the passed name, when it was created for the cluster having the passed key
func (r *ClusterReconciler) deleteOwnedCrossNamespaceRBAC(
	ctx context.Context,
	key types.NamespacedName,
	recorderObject client.Object,
	obj client.Object,
	name string,
	kind string,
) error {
	contextLogger := log.FromContext(ctx)

	if err := r.Get(ctx, client.ObjectKey{Name: name}, obj); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("while getting %s: %w", kind, err)
	}

	if !specs.IsCrossNamespaceRBACOwnedBy(obj.GetLabels(), key.Namespace, key.Name) {
		contextLogger.Info("Leaving alone an RBAC object not managed for this cluster",
			"kind", kind, "name", name)
		return nil
	}

	contextLogger.Info("Deleting RBAC object for cross-namespace resources access", "kind", kind, "name", name)
	if recorderObject != nil {
		r.Recorder.Event(recorderObject, "Normal", "Deleting"+kind,
			fmt.Sprintf("Deleting %s %s", kind, name))
	}
	if err := r.Delete(ctx, obj); err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("while deleting %s: %w", kind, err)
	}
	return nil
}

// reconcileCrossNamespaceSecretsRBAC maintains, in every namespace other than
// the one of the cluster holding DatabaseRoles of the cluster, a Role and a
// RoleBinding allowing the instance manager to read the password Secrets of
// those roles, which live in the namespace of the role.
//
// A Role or a RoleBinding having the computed name but not created for the
// cluster is left alone and reported, without failing the reconciliation:
// otherwise a tenant could block the cluster by creating it in its namespace.
func (r *ClusterReconciler) reconcileCrossNamespaceSecretsRBAC(ctx context.Context, cluster *apiv1.Cluster) error {
	var roleList apiv1.DatabaseRoleList
	if err := r.List(
		ctx,
		&roleList,
		clusterindex.MatchingCluster(client.ObjectKeyFromObject(cluster)),
	); err != nil {
		return fmt.Errorf("while listing database roles: %w", err)
	}

	rolesByNamespace := make(map[string][]apiv1.DatabaseRole)
	for i := range roleList.Items {
		role := &roleList.Items[i]
		if role.Namespace == cluster.Namespace {
			continue
		}
		rolesByNamespace[role.Namespace] = append(rolesByNamespace[role.Namespace], *role)
	}

	desiredNamespaces := make(map[string][]string, len(rolesByNamespace))
	for namespace, roles := range rolesByNamespace {
		// A rule without resource names would grant access to every Secret
		// of the namespace, so the namespaces without any Secret to read get
		// no Role at all
		if secretNames := specs.GetCrossNamespaceSecretNames(roles); len(secretNames) > 0 {
			desiredNamespaces[namespace] = secretNames
		}
	}

	for namespace, secretNames := range desiredNamespaces {
		if err := r.reconcileCrossNamespaceSecretsRole(ctx, cluster, namespace, secretNames); err != nil {
			return err
		}
		if err := r.reconcileCrossNamespaceSecretsRoleBinding(ctx, cluster, namespace); err != nil {
			return err
		}
	}

	return r.deleteCrossNamespaceSecretsRBAC(ctx, client.ObjectKeyFromObject(cluster), desiredNamespaces)
}

// reconcileCrossNamespaceSecretsRole creates the Role allowing the instance
// manager to read the password Secrets in the passed namespace, or restores
// its rules
func (r *ClusterReconciler) reconcileCrossNamespaceSecretsRole(
	ctx context.Context,
	cluster *apiv1.Cluster,
	namespace string,
	secretNames []string,
) error {
	contextLogger := log.FromContext(ctx)
	desired := specs.CreateCrossNamespaceSecretsRole(*cluster, namespace, secretNames)

	var existing rbacv1.Role
	if err := r.Get(ctx, client.ObjectKeyFromObject(&desired), &existing); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting Role %s/%s: %w", namespace, desired.Name, err)
		}

		contextLogger.Info("Creating Role for cross-namespace role password Secrets",
			"namespace", namespace, "name", desired.Name)
		r.Recorder.Event(cluster, "Normal", "CreatingRole",
			fmt.Sprintf("Creating Role %s/%s for cross-namespace role password Secrets", namespace, desired.Name))
		if err := r.Create(ctx, &desired); err != nil {
			return fmt.Errorf("while creating Role %s/%s: %w", namespace, desired.Name, err)
		}
		return nil
	}

	if !specs.IsCrossNamespaceRBACOwnedBy(existing.Labels, cluster.Namespace, cluster.Name) {
		r.reportNotOwnedCrossNamespaceRBAC(ctx, cluster, "Role", namespace, desired.Name)
		return nil
	}

	if equality.Semantic.DeepEqual(existing.Rules, desired.Rules) {
		return nil
	}

	contextLogger.Info("Updating Role for cross-namespace role password Secrets",
		"namespace", namespace, "name", desired.Name)
	patched := existing.DeepCopy()
	patched.Rules = desired.Rules
	if err := r.Patch(ctx, patched, client.MergeFrom(&existing)); err != nil {
		return fmt.Errorf("while patching Role %s/%s: %w", namespace, desired.Name, err)
	}
	return nil
}

// reconcileCrossNamespaceSecretsRoleBinding creates the RoleBinding granting
// the password Secrets Role of the passed namespace to the ServiceAccount of
// the cluster, or restores its subjects and role
func (r *ClusterReconciler) reconcileCrossNamespaceSecretsRoleBinding(
	ctx context.Context,
	cluster *apiv1.Cluster,
	namespace string,
) error {
	contextLogger := log.FromContext(ctx)
	desired := specs.CreateCrossNamespaceSecretsRoleBinding(*cluster, namespace)

	var existing rbacv1.RoleBinding
	err := r.Get(ctx, client.ObjectKeyFromObject(&desired), &existing)
	if err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("while getting RoleBinding %s/%s: %w", namespace, desired.Name, err)
	}

	if err == nil {
		if !specs.IsCrossNamespaceRBACOwnedBy(existing.Labels, cluster.Namespace, cluster.Name) {
			r.reportNotOwnedCrossNamespaceRBAC(ctx, cluster, "RoleBinding", namespace, desired.Name)
			return nil
		}

		// The role of a binding cannot be changed: a binding pointing to
		// another role is replaced
		if existing.RoleRef == desired.RoleRef {
			if equality.Semantic.DeepEqual(existing.Subjects, desired.Subjects) {
				return nil
			}

			contextLogger.Info("Updating RoleBinding for cross-namespace role password Secrets",
				"namespace", namespace, "name", desired.Name)
			patched := existing.DeepCopy()
			patched.Subjects = desired.Subjects
			if err := r.Patch(ctx, patched, client.MergeFrom(&existing)); err != nil {
				return fmt.Errorf("while patching RoleBinding %s/%s: %w", namespace, desired.Name, err)
			}
			return nil
		}

		contextLogger.Info("Replacing RoleBinding for cross-namespace role password Secrets",
			"namespace", namespace, "name", desired.Name, "roleRef", existing.RoleRef)
		if err := r.Delete(ctx, &existing); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("while deleting RoleBinding %s/%s: %w", namespace, desired.Name, err)
		}
	}

	contextLogger.Info("Creating RoleBinding for cross-namespace role password Secrets",
		"namespace", namespace, "name", desired.Name)
	r.Recorder.Event(cluster, "Normal", "CreatingRoleBinding",
		fmt.Sprintf("Creating RoleBinding %s/%s for cross-namespace role password Secrets", namespace, desired.Name))
	if err := r.Create(ctx, &desired); err != nil {
		return fmt.Errorf("while creating RoleBinding %s/%s: %w", namespace, desired.Name, err)
	}
	return nil
}

// reportNotOwnedCrossNamespaceRBAC reports a namespaced RBAC object having
// the name computed for the cluster but not created for it
func (r *ClusterReconciler) reportNotOwnedCrossNamespaceRBAC(
	ctx context.Context,
	cluster *apiv1.Cluster,
	kind, namespace, name string,
) {
	log.FromContext(ctx).Info("Leaving alone an RBAC object not managed for this cluster",
		"kind", kind, "namespace", namespace, "name", name)
	r.Recorder.Event(cluster, "Warning", "CrossNamespaceRBACNotOwned",
		fmt.Sprintf("%s %s/%s %s: the instance manager cannot read the password Secrets of "+
			"the DatabaseRoles in that namespace", kind, namespace, name, errCrossNamespaceRBACNotOwned))
}

// deleteCrossNamespaceSecretsRBAC deletes the password Secrets Roles and
// RoleBindings of the cluster having the passed key, in every namespace but
// the kept ones. Only the objects having the computed name and the labels of
// the cluster are deleted.
func (r *ClusterReconciler) deleteCrossNamespaceSecretsRBAC(
	ctx context.Context,
	key types.NamespacedName,
	keptNamespaces map[string][]string,
) error {
	contextLogger := log.FromContext(ctx)
	name := specs.GetCrossNamespaceRoleNameFor(key.Namespace, key.Name)
	selector := client.MatchingLabels(specs.CrossNamespaceRBACLabelsFor(key.Namespace, key.Name))

	isStale := func(obj client.Object) bool {
		_, kept := keptNamespaces[obj.GetNamespace()]
		return !kept &&
			obj.GetName() == name &&
			obj.GetNamespace() != key.Namespace &&
			specs.IsCrossNamespaceRBACOwnedBy(obj.GetLabels(), key.Namespace, key.Name)
	}

	var bindings rbacv1.RoleBindingList
	if err := r.List(ctx, &bindings, selector); err != nil {
		return fmt.Errorf("while listing RoleBindings: %w", err)
	}
	for i := range bindings.Items {
		binding := &bindings.Items[i]
		if !isStale(binding) {
			continue
		}
		contextLogger.Info("Deleting RoleBinding for cross-namespace role password Secrets",
			"namespace", binding.Namespace, "name", binding.Name)
		if err := r.Delete(ctx, binding); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("while deleting RoleBinding %s/%s: %w", binding.Namespace, binding.Name, err)
		}
	}

	var roles rbacv1.RoleList
	if err := r.List(ctx, &roles, selector); err != nil {
		return fmt.Errorf("while listing Roles: %w", err)
	}
	for i := range roles.Items {
		role := &roles.Items[i]
		if !isStale(role) {
			continue
		}
		contextLogger.Info("Deleting Role for cross-namespace role password Secrets",
			"namespace", role.Namespace, "name", role.Name)
		if err := r.Delete(ctx, role); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("while deleting Role %s/%s: %w", role.Namespace, role.Name, err)
		}
	}

	return nil
}
