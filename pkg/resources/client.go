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

package resources

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
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

// Create saves the object obj in the Kubernetes cluster. obj must be a
// struct pointer so that obj can be updated with the content returned by the Server.
func (e extendedClient) Create(
	ctx context.Context,
	obj client.Object,
	opts ...client.CreateOption,
) error {
	return e.Client.Create(ctx, obj, opts...)
}

// Delete deletes the given obj from Kubernetes cluster.
func (e extendedClient) Delete(
	ctx context.Context,
	obj client.Object,
	opts ...client.DeleteOption,
) error {
	return e.Client.Delete(ctx, obj, opts...)
}

// Update updates the given obj in the Kubernetes cluster. obj must be a
// struct pointer so that obj can be updated with the content returned by the Server.
func (e extendedClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	return e.Client.Update(ctx, obj, opts...)
}

// Patch patches the given obj in the Kubernetes cluster. obj must be a
// struct pointer so that obj can be updated with the content returned by the Server.
func (e extendedClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	return e.Client.Patch(ctx, obj, patch, opts...)
}
