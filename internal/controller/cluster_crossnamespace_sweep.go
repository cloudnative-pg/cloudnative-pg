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
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// crossNamespaceRBACSweepInterval is the time between two sweeps of the
// orphaned cross-namespace RBAC objects
const crossNamespaceRBACSweepInterval = 15 * time.Minute

// crossNamespaceRBACKind describes a kind of RBAC object created for the
// cross-namespace resources support and labelled with the owning cluster
type crossNamespaceRBACKind struct {
	name          string
	clusterScoped bool
	newList       func() client.ObjectList
}

// crossNamespaceRBACKinds are the kinds of RBAC objects swept, in deletion
// order: the bindings go before the roles they refer to. The Roles and the
// RoleBindings are the ones granting access to the password Secrets of the
// tenant namespaces.
var crossNamespaceRBACKinds = []crossNamespaceRBACKind{
	{
		name:          "ClusterRoleBinding",
		clusterScoped: true,
		newList:       func() client.ObjectList { return &rbacv1.ClusterRoleBindingList{} },
	},
	{name: "RoleBinding", newList: func() client.ObjectList { return &rbacv1.RoleBindingList{} }},
	{
		name:          "ClusterRole",
		clusterScoped: true,
		newList:       func() client.ObjectList { return &rbacv1.ClusterRoleList{} },
	},
	{name: "Role", newList: func() client.ObjectList { return &rbacv1.RoleList{} }},
}

// crossNamespaceRBACObject is an RBAC object found by the sweep
type crossNamespaceRBACObject struct {
	kind string
	obj  client.Object
}

// CrossNamespaceRBACSweeper deletes the RBAC objects created for the
// cross-namespace resources support of clusters that no longer exist, or
// that do not enable the feature.
//
// Those objects are cluster-scoped, or live in the namespaces of the tenants,
// and cannot be garbage collected through an owner reference to the Cluster.
// They are deleted when the deletion of the Cluster is reconciled, but a
// Cluster deleted while the operator is down, with no dependent left to
// enqueue it, is never reconciled again, and a new Cluster having the same
// name must not inherit the access granted to the previous one: the sweep,
// run at startup and periodically, removes what is left.
type CrossNamespaceRBACSweeper struct {
	// client lists and deletes the RBAC objects
	client client.Client

	// reader reads the clusters without the cache, which may not be
	// synchronized yet at startup and would report a cluster as missing
	reader client.Reader

	// watchedNamespaces restricts the sweep to the clusters of these
	// namespaces. Empty means every namespace.
	watchedNamespaces []string

	// clusterRBACForbidden skips the ClusterRoles and the ClusterRoleBindings,
	// which the operator cannot list
	clusterRBACForbidden bool

	interval time.Duration
}

var _ manager.LeaderElectionRunnable = &CrossNamespaceRBACSweeper{}

// NewCrossNamespaceRBACSweeper creates a new sweeper. canManageClusterRBAC
// tells whether the operator can list and watch ClusterRoles and
// ClusterRoleBindings: when it cannot, only the Roles and the RoleBindings of
// the tenant namespaces are swept.
func NewCrossNamespaceRBACSweeper(
	cli client.Client,
	reader client.Reader,
	watchedNamespaces []string,
	canManageClusterRBAC bool,
) *CrossNamespaceRBACSweeper {
	return &CrossNamespaceRBACSweeper{
		client:               cli,
		reader:               reader,
		watchedNamespaces:    watchedNamespaces,
		clusterRBACForbidden: !canManageClusterRBAC,
		interval:             crossNamespaceRBACSweepInterval,
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable: only the
// leader deletes objects
func (s *CrossNamespaceRBACSweeper) NeedLeaderElection() bool {
	return true
}

// Start implements manager.Runnable, sweeping once and then periodically
// until the context is cancelled. Sweep errors are logged and retried at the
// next run, as returning them would stop the manager.
func (s *CrossNamespaceRBACSweeper) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx).WithName("cross-namespace-rbac-sweeper")
	ctx = log.IntoContext(ctx, contextLogger)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		if err := s.Sweep(ctx); err != nil {
			contextLogger.Error(err, "while sweeping orphaned cross-namespace RBAC objects")
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Sweep deletes the cross-namespace RBAC objects of the clusters that no
// longer exist or do not enable the feature. A cluster that cannot be read,
// for an error other than NotFound, is left alone.
func (s *CrossNamespaceRBACSweeper) Sweep(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	objectsByCluster, clusterKeys, err := s.listByCluster(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, key := range clusterKeys {
		orphaned, err := s.isOrphaned(ctx, key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !orphaned {
			continue
		}

		for _, object := range objectsByCluster[key] {
			if err := s.deleteOrphaned(ctx, key, object); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) == 0 && len(clusterKeys) > 0 {
		contextLogger.Debug("Swept cross-namespace RBAC objects", "clusters", len(clusterKeys))
	}
	return errors.Join(errs...)
}

// listByCluster lists the cross-namespace RBAC objects of every kind swept,
// grouped by the cluster of the watched namespaces they were created for.
// The keys of the clusters are returned in the order they were found.
func (s *CrossNamespaceRBACSweeper) listByCluster(
	ctx context.Context,
) (map[types.NamespacedName][]crossNamespaceRBACObject, []types.NamespacedName, error) {
	selector, err := specs.CrossNamespaceRBACSelector()
	if err != nil {
		return nil, nil, err
	}

	objectsByCluster := make(map[types.NamespacedName][]crossNamespaceRBACObject)
	var clusterKeys []types.NamespacedName
	for _, kind := range crossNamespaceRBACKinds {
		if kind.clusterScoped && s.clusterRBACForbidden {
			continue
		}

		list := kind.newList()
		if err := s.client.List(ctx, list, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, fmt.Errorf("while listing %s objects: %w", kind.name, err)
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return nil, nil, fmt.Errorf("while reading %s objects: %w", kind.name, err)
		}
		for _, item := range items {
			obj, ok := item.(client.Object)
			if !ok {
				continue
			}
			key := types.NamespacedName{
				Namespace: obj.GetLabels()[utils.ClusterNamespaceLabelName],
				Name:      obj.GetLabels()[utils.ClusterLabelName],
			}
			if !s.isWatched(key.Namespace) {
				continue
			}
			if _, found := objectsByCluster[key]; !found {
				clusterKeys = append(clusterKeys, key)
			}
			objectsByCluster[key] = append(objectsByCluster[key], crossNamespaceRBACObject{kind: kind.name, obj: obj})
		}
	}

	return objectsByCluster, clusterKeys, nil
}

// isOrphaned tells whether the cluster having the passed key no longer needs
// its cross-namespace RBAC objects: it does not exist, or it does not enable
// the feature. A cluster that cannot be read is reported as an error.
func (s *CrossNamespaceRBACSweeper) isOrphaned(ctx context.Context, key types.NamespacedName) (bool, error) {
	var cluster apiv1.Cluster
	err := s.reader.Get(ctx, key, &cluster)
	switch {
	case apierrs.IsNotFound(err):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("while checking whether cluster %s exists: %w", key, err)
	default:
		return !cluster.Spec.EnableCrossNamespaceResources, nil
	}
}

// deleteOrphaned deletes an RBAC object created for the passed cluster,
// which no longer exists or does not enable the feature. Only the objects
// having the name computed for the cluster are deleted: the labels alone
// could also match other objects of the operator.
func (s *CrossNamespaceRBACSweeper) deleteOrphaned(
	ctx context.Context,
	key types.NamespacedName,
	object crossNamespaceRBACObject,
) error {
	obj, kind := object.obj, object.kind
	if obj.GetName() != specs.GetCrossNamespaceRoleNameFor(key.Namespace, key.Name) ||
		!specs.IsCrossNamespaceRBACOwnedBy(obj.GetLabels(), key.Namespace, key.Name) {
		return nil
	}

	log.FromContext(ctx).Info("Deleting orphaned RBAC object for cross-namespace resources access",
		"kind", kind, "namespace", obj.GetNamespace(), "name", obj.GetName(), "cluster", key)
	if err := s.client.Delete(ctx, obj); err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("while deleting %s %s of cluster %s: %w", kind, obj.GetName(), key, err)
	}
	return nil
}

// isWatched tells whether the clusters of the passed namespace are managed
// by this operator
func (s *CrossNamespaceRBACSweeper) isWatched(namespace string) bool {
	return len(s.watchedNamespaces) == 0 || slices.Contains(s.watchedNamespaces, namespace)
}
