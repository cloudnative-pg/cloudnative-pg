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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type finalizerReconciler[T client.Object] struct {
	cli           client.Client
	finalizerName string
	onRemoveFunc  func(ctx context.Context, resource T) error
}

func newFinalizerReconciler[T client.Object](
	cli client.Client,
	finalizerName string,
	onRemoveFunc func(ctx context.Context, resource T) error,
) *finalizerReconciler[T] {
	return &finalizerReconciler[T]{
		cli:           cli,
		finalizerName: finalizerName,
		onRemoveFunc:  onRemoveFunc,
	}
}

func (f finalizerReconciler[T]) reconcile(ctx context.Context, resource T) error {
	// add finalizer in non-deleted publications if not present
	if resource.GetDeletionTimestamp().IsZero() {
		if !controllerutil.AddFinalizer(resource, f.finalizerName) {
			return nil
		}
		return f.cli.Update(ctx, resource)
	}

	// the publication is being deleted but no finalizer is present, we can quit
	if !controllerutil.ContainsFinalizer(resource, f.finalizerName) {
		return nil
	}

	if err := f.onRemoveFunc(ctx, resource); err != nil {
		return err
	}

	// remove our finalizer from the list and update it.
	controllerutil.RemoveFinalizer(resource, f.finalizerName)
	return f.cli.Update(ctx, resource)
}
