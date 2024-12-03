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

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

type managerObject interface {
	GetName() string
	GetManagedObjectName() string
	GetClusterRef() corev1.LocalObjectReference
	HasReconciliations() bool
}

// detectTooManyManagers checks if a certain `manager` wants to manage the same
// underlying object as some previously existing manager
//
//	also known as turf war
func detectTooManyManagers(
	manager managerObject,
	potentialManagers []managerObject,
) error {
	for _, item := range potentialManagers {
		if item.GetName() == manager.GetName() {
			// skip itself
			continue
		}

		if item.GetClusterRef().Name != manager.GetClusterRef().Name {
			continue
		}

		if !item.HasReconciliations() {
			continue
		}

		if item.GetManagedObjectName() == manager.GetManagedObjectName() {
			return fmt.Errorf("%q is already managed by object %q",
				manager.GetManagedObjectName(), item.GetName())
		}
	}

	return nil
}

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
