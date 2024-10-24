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
	"fmt"
	"sort"

	"github.com/lib/pq"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

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
	// create slice and store keys
	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		keys = append(keys, k)
	}

	// sort the slice by keys
	sort.Strings(keys)

	b := new(bytes.Buffer)
	for _, key := range keys {
		// TODO(armru): should we sanitize the key?
		// TODO(armru): any alternative to pg.QuoteLiteral?
		_, _ = fmt.Fprintf(b, "%s = %s, ", key, pq.QuoteLiteral(parameters[key]))
	}

	// pruning last 2 chars `, `
	return b.String()[:len(b.String())-2]
}
