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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// errClusterIsReplica is raised when an object
// cannot be reconciled because it belongs to a replica cluster
var errClusterIsReplica = fmt.Errorf("waiting for the cluster to become primary")

type instanceInterface interface {
	GetSuperUserDB() (*sql.DB, error)
	GetClusterName() string
	GetPodName() string
	GetNamespaceName() string
}

type markableAsFailed interface {
	client.Object
	SetAsFailed(err error)
}

// markAsFailed marks the reconciliation as failed and logs the corresponding error
func markAsFailed(
	ctx context.Context,
	cli client.Client,
	resource markableAsFailed,
	err error,
) error {
	resource.SetAsFailed(err)
	return cli.Status().Update(ctx, resource)
}

type markableAsUnknown interface {
	client.Object
	SetAsUnknown(err error)
}

// markAsUnknown marks the reconciliation as failed and logs the corresponding error
func markAsUnknown(
	ctx context.Context,
	cli client.Client,
	resource markableAsUnknown,
	err error,
) error {
	resource.SetAsUnknown(err)
	return cli.Status().Update(ctx, resource)
}

type replicaTransitionAware interface {
	markableAsUnknown
	GetStatusApplied() *bool
}

// handleReplicaRoleTransition decides what to do with a resource whose
// current generation has already been reconciled, reacting to its cluster
// moving in or out of the replica role.
//
// When the cluster is demoted, the designated primary reports the replica
// condition on the resource, keeping the recorded reconciliation so the
// resource retains the ownership of the Postgres object it manages. When the
// cluster is promoted back (or the last evaluation didn't succeed), it asks
// the caller to evaluate the resource again by returning proceed=true; the
// regular reconciliation flow then decides which pod acts.
func handleReplicaRoleTransition(
	ctx context.Context,
	cli client.Client,
	instance instanceInterface,
	cluster *apiv1.Cluster,
	resource replicaTransitionAware,
	requeueAfter time.Duration,
) (result ctrl.Result, proceed bool, err error) {
	if !resource.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, false, nil
	}

	applied := resource.GetStatusApplied()

	if !cluster.IsReplica() {
		proceed := applied == nil || !*applied
		return ctrl.Result{}, proceed, nil
	}

	isDesignatedPrimary := cluster.Status.CurrentPrimary == instance.GetPodName()

	if isDesignatedPrimary && applied != nil {
		if err := markAsUnknown(ctx, cli, resource, errClusterIsReplica); err != nil {
			return ctrl.Result{}, false, err
		}
	}

	// Keep polling while the designated primary awaits the promotion, or
	// while the primary recorded in the cluster status is still settling
	// (e.g. a failover concurrent with the demotion): status-only updates
	// don't retrigger the cluster watch.
	if isDesignatedPrimary || applied != nil {
		return ctrl.Result{RequeueAfter: requeueAfter}, false, nil
	}

	return ctrl.Result{}, false, nil
}

type markableAsReady interface {
	client.Object
	SetAsReady()
}

// markAsReady marks the reconciliation as succeeded inside the resource
func markAsReady(
	ctx context.Context,
	cli client.Client,
	resource markableAsReady,
) error {
	resource.SetAsReady()
	return cli.Status().Update(ctx, resource)
}

// clusterScopedResource is a resource bound to a Cluster through a local
// object reference.
type clusterScopedResource interface {
	client.Object
	GetClusterRef() corev1.LocalObjectReference
}

// mapClusterToManagedResources builds a watch mapper that enqueues every item
// of the given list type targeting this instance's cluster, to react to
// cluster spec changes (e.g. a demotion to replica).
func mapClusterToManagedResources(
	instance instanceInterface,
	cli client.Client,
	newList func() client.ObjectList,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetName() != instance.GetClusterName() ||
			obj.GetNamespace() != instance.GetNamespaceName() {
			return nil
		}

		list := newList()
		if err := cli.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
			log.FromContext(ctx).Error(err, "while listing resources to react to a cluster change",
				"kind", fmt.Sprintf("%T", list))
			return nil
		}

		items, err := apimeta.ExtractList(list)
		if err != nil {
			log.FromContext(ctx).Error(err, "while extracting the resource list",
				"kind", fmt.Sprintf("%T", list))
			return nil
		}

		requests := make([]reconcile.Request, 0, len(items))
		for _, item := range items {
			resource, ok := item.(clusterScopedResource)
			if !ok || resource.GetClusterRef().Name != obj.GetName() {
				continue
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: resource.GetNamespace(),
					Name:      resource.GetName(),
				},
			})
		}

		return requests
	}
}

func getClusterFromInstance(
	ctx context.Context,
	cli client.Client,
	instance instanceInterface,
) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := cli.Get(ctx, types.NamespacedName{
		Name:      instance.GetClusterName(),
		Namespace: instance.GetNamespaceName(),
	}, &cluster)
	return &cluster, err
}

func toPostgresParameters(parameters map[string]string) string {
	if len(parameters) == 0 {
		return ""
	}

	b := new(bytes.Buffer)
	for _, key := range slices.Sorted(maps.Keys(parameters)) {
		// TODO(armru): any alternative to pg.QuoteLiteral?
		_, _ = fmt.Fprintf(b, "%s = %s, ", pgx.Identifier{key}.Sanitize(), pq.QuoteLiteral(parameters[key]))
	}

	// pruning last 2 chars `, `
	return b.String()[:len(b.String())-2]
}

type postgresResourceManager interface {
	client.Object
	HasReconciliations() bool
	markableAsFailed
}

type managedResourceExclusivityEnsurer[T postgresResourceManager] interface {
	MustHaveManagedResourceExclusivity(newManager T) error
	client.ObjectList
}

func detectConflictingManagers[T postgresResourceManager, TL managedResourceExclusivityEnsurer[T]](
	ctx context.Context,
	cli client.Client,
	resource T,
	list TL,
) (ctrl.Result, error) {
	if resource.HasReconciliations() {
		return ctrl.Result{}, nil
	}
	contextLogger := log.FromContext(ctx)

	if err := cli.List(ctx, list,
		client.InNamespace(resource.GetNamespace()),
	); err != nil {
		kind := list.GetObjectKind().GroupVersionKind().Kind

		contextLogger.Error(err, "while getting list",
			"kind", kind,
			"namespace", resource.GetNamespace(),
		)
		return ctrl.Result{}, fmt.Errorf("impossible to list %s objects in namespace %s: %w",
			kind, resource.GetNamespace(), err)
	}

	// Make sure the target PG element is not being managed by another kubernetes resource
	if conflictErr := list.MustHaveManagedResourceExclusivity(resource); conflictErr != nil {
		if markErr := markAsFailed(ctx, cli, resource, conflictErr); markErr != nil {
			return ctrl.Result{},
				fmt.Errorf("encountered an error while marking as failed the resource: %w, original error: %w",
					markErr,
					conflictErr,
				)
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func parseOptions(raw []string) (map[string]string, error) {
	opts := make(map[string]string, len(raw))
	for _, opt := range raw {
		parts := strings.SplitN(opt, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf(
				"unparsable option: expected \"keyword=value\", got %v",
				raw,
			)
		}
		opts[parts[0]] = parts[1]
	}
	return opts, nil
}
