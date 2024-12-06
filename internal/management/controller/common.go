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

package controller

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	oldResource := resource.DeepCopyObject().(markableAsFailed)
	resource.SetAsFailed(err)
	return cli.Status().Patch(ctx, resource, client.MergeFrom(oldResource))
}

type markableAsUnknown interface {
	client.Object
	SetAsUnknown(err error)
}

// markAsFailed marks the reconciliation as failed and logs the corresponding error
func markAsUnknown(
	ctx context.Context,
	cli client.Client,
	resource markableAsUnknown,
	err error,
) error {
	oldResource := resource.DeepCopyObject().(markableAsUnknown)
	resource.SetAsUnknown(err)
	return cli.Status().Patch(ctx, resource, client.MergeFrom(oldResource))
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
	oldResource := resource.DeepCopyObject().(markableAsReady)
	resource.SetAsReady()

	return cli.Status().Patch(ctx, resource, client.MergeFrom(oldResource))
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
