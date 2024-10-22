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
	"context"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
