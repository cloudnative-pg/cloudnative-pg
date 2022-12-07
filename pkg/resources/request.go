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

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type patchCondition[T client.Object] func(ctx context.Context, proposed, current T) bool

// Request is a client request that adheres to the client.Client interface
type Request[T client.Object] struct {
	shouldCreate    bool
	patchConditions []patchCondition[T]
	patchType       PatchType
	c               client.Client
}

// NewRequest instantiate a new request with the passed client
func NewRequest[T client.Object](c client.Client) *Request[T] {
	return &Request[T]{c: c, patchType: PatchTypeStrategicMerge}
}

// CreateIfNotFound creates the given object if it doesn't already exist
func CreateIfNotFound[T client.Object](ctx context.Context, c client.Client, obj T) error {
	return NewRequest[T](c).
		CreateIfNotFound().
		Execute(ctx, obj)
}

// Execute will execute the request with the given instructions
func (r *Request[T]) Execute(
	ctx context.Context,
	proposed T,
) error {
	// Try getting the object from the cluster to discover if we need
	// to create a new object or updating an existing one
	current := proposed.DeepCopyObject().(T)

	// Get the current status of the object
	err := r.c.Get(ctx, types.NamespacedName{Namespace: proposed.GetNamespace(), Name: proposed.GetName()}, current)
	switch {
	case apierrs.IsNotFound(err) && r.shouldCreate:
		// The object doesn't exist in the cluster, so we create it
		return r.c.Create(ctx, proposed)

	case err != nil:
		// We can't get the object from the cluster, and we don't know
		// if the object really exists or not. Better return this error
		// to the user: perhaps some permissions are missing
		return err
	}

	for _, condition := range r.patchConditions {
		if condition(ctx, proposed, current) {
			// We have the current status of the object, so we patch it with
			// the current version. The passed object may be newly generated, so
			// we fill its GroupVersionKind.
			proposed.GetObjectKind().SetGroupVersionKind(current.GetObjectKind().GroupVersionKind())
			// ensure that there is always a resource version
			if proposed.GetResourceVersion() == "" {
				proposed.SetResourceVersion(current.GetResourceVersion())
			}

			return r.c.Patch(ctx, proposed, r.patchType.BuildPatch(current))
		}
	}

	return nil
}

// CreateIfNotFound will attempt to create the resource if it receives IsNotFound error
func (r *Request[T]) CreateIfNotFound() *Request[T] {
	r.shouldCreate = true
	return r
}

// PatchAlways will always try to attempt the patch of the retrieved resource with the proposed one
func (r *Request[T]) PatchAlways() *Request[T] {
	r.patchConditions = append(r.patchConditions, func(ctx context.Context, current, proposed T) bool { return true })
	return r
}

// WithCustomPatchConditions will evaluate custom patch conditions
func (r *Request[T]) WithCustomPatchConditions(conditions ...patchCondition[T]) *Request[T] {
	r.patchConditions = append(r.patchConditions, conditions...)
	return r
}
