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

// crossNamespaceResourcesPurpose and crossNamespaceSecretsPurpose describe,
// in the logs and the events, what the RBAC objects are created for
const (
	crossNamespaceResourcesPurpose = "cross-namespace resources access"
	crossNamespaceSecretsPurpose   = "cross-namespace role password Secrets"
)

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

	clusterRole := specs.CreateCrossNamespaceRole(*cluster)
	if err := r.ensureCrossNamespaceRBACObject(
		ctx, cluster, &clusterRole, &rbacv1.ClusterRole{}, crossNamespaceResourcesPurpose,
	); err != nil {
		return err
	}

	clusterRoleBinding := specs.CreateCrossNamespaceRoleBinding(*cluster)
	if err := r.ensureCrossNamespaceRBACObject(
		ctx, cluster, &clusterRoleBinding, &rbacv1.ClusterRoleBinding{}, crossNamespaceResourcesPurpose,
	); err != nil {
		return err
	}

	return r.reconcileCrossNamespaceSecretsRBAC(ctx, cluster)
}

// ensureCrossNamespaceRBACObject creates the desired Role, ClusterRole,
// RoleBinding or ClusterRoleBinding, or restores the rules of a role and the
// subjects of a binding. The role of a binding cannot be changed, so a
// binding referring to another role is replaced.
//
// The existing parameter is an empty object of the same kind, used to read
// the current state. When an object having the desired name exists but was
// not created for the cluster, errCrossNamespaceRBACNotOwned is returned and
// the object is left alone.
func (r *ClusterReconciler) ensureCrossNamespaceRBACObject(
	ctx context.Context,
	cluster *apiv1.Cluster,
	desired client.Object,
	existing client.Object,
	purpose string,
) error {
	kind, name := crossNamespaceRBACKindOf(desired), crossNamespaceRBACDisplayName(desired)
	contextLogger := log.FromContext(ctx).WithValues("kind", kind, "name", name)

	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("while getting %s %s: %w", kind, name, err)
	}

	if err == nil {
		if !specs.IsCrossNamespaceRBACOwnedBy(existing.GetLabels(), cluster.Namespace, cluster.Name) {
			return fmt.Errorf("%s %s %w", kind, name, errCrossNamespaceRBACNotOwned)
		}

		replace, err := r.repairCrossNamespaceRBACObject(ctx, existing, desired, purpose)
		if err != nil || !replace {
			return err
		}

		contextLogger.Info("Replacing a binding referring to another role for " + purpose)
		if err := r.Delete(ctx, existing); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("while deleting %s %s: %w", kind, name, err)
		}
	}

	contextLogger.Info("Creating " + kind + " for " + purpose)
	r.Recorder.Event(cluster, "Normal", "Creating"+kind,
		fmt.Sprintf("Creating %s %s for %s", kind, name, purpose))
	if err := r.Create(ctx, desired); err != nil {
		// Only the cluster-scoped objects created by the operator are cached:
		// one having the same name but not their labels cannot be read, and
		// is only detected when creating the desired one
		if apierrs.IsAlreadyExists(err) {
			return fmt.Errorf("%s %s %w", kind, name, errCrossNamespaceRBACNotOwned)
		}
		return fmt.Errorf("while creating %s %s: %w", kind, name, err)
	}
	return nil
}

// repairCrossNamespaceRBACObject restores the rules of an existing role, or
// the subjects of an existing binding, to the desired ones. It returns true
// when the existing object is a binding referring to another role, which
// must be replaced since the role of a binding cannot be changed.
func (r *ClusterReconciler) repairCrossNamespaceRBACObject(
	ctx context.Context,
	existing client.Object,
	desired client.Object,
	purpose string,
) (bool, error) {
	patched, ok := existing.DeepCopyObject().(client.Object)
	if !ok {
		return false, fmt.Errorf("unexpected RBAC object %T", existing)
	}

	switch target := patched.(type) {
	case *rbacv1.ClusterRole:
		desiredRules := desired.(*rbacv1.ClusterRole).Rules
		if equality.Semantic.DeepEqual(target.Rules, desiredRules) {
			return false, nil
		}
		target.Rules = desiredRules

	case *rbacv1.Role:
		desiredRules := desired.(*rbacv1.Role).Rules
		if equality.Semantic.DeepEqual(target.Rules, desiredRules) {
			return false, nil
		}
		target.Rules = desiredRules

	case *rbacv1.ClusterRoleBinding:
		desiredBinding := desired.(*rbacv1.ClusterRoleBinding)
		if target.RoleRef != desiredBinding.RoleRef {
			return true, nil
		}
		if equality.Semantic.DeepEqual(target.Subjects, desiredBinding.Subjects) {
			return false, nil
		}
		target.Subjects = desiredBinding.Subjects

	case *rbacv1.RoleBinding:
		desiredBinding := desired.(*rbacv1.RoleBinding)
		if target.RoleRef != desiredBinding.RoleRef {
			return true, nil
		}
		if equality.Semantic.DeepEqual(target.Subjects, desiredBinding.Subjects) {
			return false, nil
		}
		target.Subjects = desiredBinding.Subjects

	default:
		return false, fmt.Errorf("unexpected RBAC object %T", existing)
	}

	kind, name := crossNamespaceRBACKindOf(desired), crossNamespaceRBACDisplayName(desired)
	log.FromContext(ctx).Info("Updating "+kind+" for "+purpose, "kind", kind, "name", name)
	if err := r.Patch(ctx, patched, client.MergeFrom(existing)); err != nil {
		return false, fmt.Errorf("while patching %s %s: %w", kind, name, err)
	}
	return false, nil
}

// crossNamespaceRBACKindOf returns the kind of an RBAC object, whose type
// information is not set on the objects built by the operator
func crossNamespaceRBACKindOf(obj client.Object) string {
	switch obj.(type) {
	case *rbacv1.ClusterRole:
		return "ClusterRole"
	case *rbacv1.ClusterRoleBinding:
		return "ClusterRoleBinding"
	case *rbacv1.Role:
		return "Role"
	case *rbacv1.RoleBinding:
		return "RoleBinding"
	default:
		return fmt.Sprintf("%T", obj)
	}
}

// crossNamespaceRBACDisplayName returns the name of an RBAC object as shown
// in the logs, the errors and the events
func crossNamespaceRBACDisplayName(obj client.Object) string {
	if obj.GetNamespace() == "" {
		return obj.GetName()
	}
	return obj.GetNamespace() + "/" + obj.GetName()
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

	// Reading them would hang on an informer the API server refuses. An
	// operator lacking that access cannot have created them either.
	if r.clusterRBACForbidden {
		return nil
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
// A failure in one namespace, such as one being terminated, does not prevent
// the other namespaces from being reconciled: the failures are returned
// together once every namespace was handled.
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

	var errs []error
	for namespace, secretNames := range desiredNamespaces {
		if err := r.reconcileCrossNamespaceSecretsNamespace(ctx, cluster, namespace, secretNames); err != nil {
			errs = append(errs, err)
		}
	}

	if err := r.deleteCrossNamespaceSecretsRBAC(ctx, client.ObjectKeyFromObject(cluster), desiredNamespaces); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// reconcileCrossNamespaceSecretsNamespace creates or repairs, in the passed
// namespace, the Role allowing the instance manager to read the passed
// password Secrets and the RoleBinding granting it to the ServiceAccount of
// the cluster. When the Role was not created for the cluster, no binding to
// it is made: its rules are not under the control of the operator.
func (r *ClusterReconciler) reconcileCrossNamespaceSecretsNamespace(
	ctx context.Context,
	cluster *apiv1.Cluster,
	namespace string,
	secretNames []string,
) error {
	role := specs.CreateCrossNamespaceSecretsRole(*cluster, namespace, secretNames)
	err := r.ensureCrossNamespaceRBACObject(ctx, cluster, &role, &rbacv1.Role{}, crossNamespaceSecretsPurpose)
	if errors.Is(err, errCrossNamespaceRBACNotOwned) {
		r.reportNotOwnedCrossNamespaceRBAC(ctx, cluster, err)
		return nil
	}
	if err != nil {
		return err
	}

	binding := specs.CreateCrossNamespaceSecretsRoleBinding(*cluster, namespace)
	err = r.ensureCrossNamespaceRBACObject(ctx, cluster, &binding, &rbacv1.RoleBinding{}, crossNamespaceSecretsPurpose)
	if errors.Is(err, errCrossNamespaceRBACNotOwned) {
		r.reportNotOwnedCrossNamespaceRBAC(ctx, cluster, err)
		return nil
	}
	return err
}

// reportNotOwnedCrossNamespaceRBAC reports a namespaced RBAC object having
// the name computed for the cluster but not created for it
func (r *ClusterReconciler) reportNotOwnedCrossNamespaceRBAC(
	ctx context.Context,
	cluster *apiv1.Cluster,
	err error,
) {
	log.FromContext(ctx).Info("Leaving alone an RBAC object not managed for this cluster", "reason", err.Error())
	r.Recorder.Event(cluster, "Warning", "CrossNamespaceRBACNotOwned",
		fmt.Sprintf("%s: the instance manager cannot read the password Secrets of "+
			"the DatabaseRoles in that namespace", err))
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
