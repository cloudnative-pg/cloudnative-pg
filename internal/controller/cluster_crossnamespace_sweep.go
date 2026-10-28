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
	name    string
	newList func() client.ObjectList
}

// crossNamespaceRBACKinds are the kinds of RBAC objects swept, in deletion
// order: the bindings go before the roles they refer to
var crossNamespaceRBACKinds = []crossNamespaceRBACKind{
	{name: "ClusterRoleBinding", newList: func() client.ObjectList { return &rbacv1.ClusterRoleBindingList{} }},
	{name: "ClusterRole", newList: func() client.ObjectList { return &rbacv1.ClusterRoleList{} }},
}

// crossNamespaceRBACObject is an RBAC object found by the sweep
type crossNamespaceRBACObject struct {
	kind string
	obj  client.Object
}

// CrossNamespaceRBACSweeper deletes the RBAC objects created for the
// cross-namespace resources support of clusters that no longer exist.
//
// Those objects are cluster-scoped and cannot be garbage collected through an
// owner reference to the Cluster. They are deleted when the deletion of the
// Cluster is reconciled, but a Cluster deleted while the operator is down,
// with no dependent left to enqueue it, is never reconciled again: the sweep,
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

	interval time.Duration
}

var _ manager.LeaderElectionRunnable = &CrossNamespaceRBACSweeper{}

// NewCrossNamespaceRBACSweeper creates a new sweeper
func NewCrossNamespaceRBACSweeper(
	cli client.Client,
	reader client.Reader,
	watchedNamespaces []string,
) *CrossNamespaceRBACSweeper {
	return &CrossNamespaceRBACSweeper{
		client:            cli,
		reader:            reader,
		watchedNamespaces: watchedNamespaces,
		interval:          crossNamespaceRBACSweepInterval,
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
// longer exist. A cluster whose existence cannot be confirmed, for an error
// other than NotFound, is left alone.
func (s *CrossNamespaceRBACSweeper) Sweep(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	selector, err := specs.CrossNamespaceRBACSelector()
	if err != nil {
		return err
	}

	// The objects of every kind, grouped by the cluster they were created for
	objectsByCluster := make(map[types.NamespacedName][]crossNamespaceRBACObject)
	var clusterKeys []types.NamespacedName
	for _, kind := range crossNamespaceRBACKinds {
		list := kind.newList()
		if err := s.client.List(ctx, list, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return fmt.Errorf("while listing %s objects: %w", kind.name, err)
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return fmt.Errorf("while reading %s objects: %w", kind.name, err)
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

	var errs []error
	for _, key := range clusterKeys {
		err := s.reader.Get(ctx, key, &apiv1.Cluster{})
		if err == nil {
			continue
		}
		if !apierrs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("while checking whether cluster %s exists: %w", key, err))
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

// deleteOrphaned deletes an RBAC object created for the passed cluster,
// which no longer exists
func (s *CrossNamespaceRBACSweeper) deleteOrphaned(
	ctx context.Context,
	key types.NamespacedName,
	object crossNamespaceRBACObject,
) error {
	obj, kind := object.obj, object.kind
	if !specs.IsCrossNamespaceRBACOwnedBy(obj.GetLabels(), key.Namespace, key.Name) {
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
