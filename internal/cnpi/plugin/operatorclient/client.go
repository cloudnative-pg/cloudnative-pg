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

package operatorclient

import (
	"context"
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	cnpgiClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	contextutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils/context"
)

type extendedClient struct {
	client.Client
}

// NewExtendedClient returns a client.Client capable of interacting with the plugin feature
func NewExtendedClient(c client.Client) client.Client {
	return &extendedClient{
		Client: c,
	}
}

func (e *extendedClient) invokePlugin(
	ctx context.Context,
	operationVerb plugin.OperationVerb,
	obj client.Object,
) (client.Object, error) {
	contextLogger := log.FromContext(ctx).WithName("invokePlugin")

	cluster, ok := ctx.Value(contextutils.ContextKeyCluster).(client.Object)
	if !ok || cluster == nil {
		contextLogger.Trace("skipping invokePlugin, cannot find the cluster inside the context")
		return obj, nil
	}

	pluginClient := cnpgiClient.GetPluginClientFromContext(ctx)
	if pluginClient == nil {
		contextLogger.Trace("skipping invokePlugin, cannot find the plugin client inside the context")
		return obj, nil
	}

	contextLogger.Trace("correctly loaded the plugin client")
	return pluginClient.LifecycleHook(ctx, operationVerb, cluster, obj)
}

// Create saves the object obj in the Kubernetes cluster. obj must be a
// struct pointer so that obj can be updated with the content returned by the Server.
func (e *extendedClient) Create(
	ctx context.Context,
	obj client.Object,
	opts ...client.CreateOption,
) error {
	var err error
	obj, err = e.invokePlugin(ctx, plugin.OperationVerbCreate, obj)
	if err != nil {
		return err
	}
	return e.Client.Create(ctx, obj, opts...)
}

// Delete deletes the given obj from Kubernetes cluster.
func (e *extendedClient) Delete(
	ctx context.Context,
	obj client.Object,
	opts ...client.DeleteOption,
) error {
	contextLogger := log.FromContext(ctx).WithName("extended_client_delete")

	origObj := obj.DeepCopyObject().(client.Object)
	var err error
	obj, err = e.invokePlugin(ctx, plugin.OperationVerbDelete, obj)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(origObj, obj) {
		if err := e.Client.Patch(ctx, obj, client.MergeFrom(origObj)); err != nil && !apierrors.IsNotFound(err) {
			contextLogger.Error(err, "while patching before delete")
			return err
		}
	}

	return e.Client.Delete(ctx, obj, opts...)
}

// Update updates the given obj in the Kubernetes cluster. obj must be a
// struct pointer so that obj can be updated with the content returned by the Server.
func (e *extendedClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	var err error
	obj, err = e.invokePlugin(ctx, plugin.OperationVerbUpdate, obj)
	if err != nil {
		return err
	}
	return e.Client.Update(ctx, obj, opts...)
}

// Patch patches the given obj in the Kubernetes cluster. obj must be a
// struct pointer so that obj can be updated with the content returned by the Server.
func (e *extendedClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	var err error
	obj, err = e.invokePlugin(ctx, plugin.OperationVerbPatch, obj)
	if err != nil {
		return err
	}
	return e.Client.Patch(ctx, obj, patch, opts...)
}
