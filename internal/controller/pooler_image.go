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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
)

// resolvePoolerImage determines the pgbouncer image that the resulting
// Deployment will run, applying the following priority (highest wins):
//
//  1. an explicit image set on the pgbouncer container in spec.template
//  2. spec.pgbouncer.image
//  3. spec.pgbouncer.imageCatalogRef
//  4. operator default (config.Current.PgbouncerImageName)
//
// The pod-template override is honoured here so that status.image always
// matches what is actually written into the Deployment by the builder
// (which keeps any image already set on the pgbouncer container in
// spec.template).
func (r *PoolerReconciler) resolvePoolerImage(ctx context.Context, pooler *apiv1.Pooler) (string, error) {
	if image := podTemplatePgbouncerImage(pooler); image != "" {
		return image, nil
	}

	if pooler.Spec.PgBouncer == nil {
		return configuration.Current.PgbouncerImageName, nil
	}

	if pooler.Spec.PgBouncer.ImageCatalogRef != nil {
		return r.resolveImageFromCatalog(ctx, pooler)
	}

	if pooler.Spec.PgBouncer.Image != "" {
		return pooler.Spec.PgBouncer.Image, nil
	}

	return configuration.Current.PgbouncerImageName, nil
}

// podTemplatePgbouncerImage returns a non-empty image when the user has pinned
// the pgbouncer container image in spec.template; the deployment builder
// preserves that value, so it must win over every other resolution source.
func podTemplatePgbouncerImage(pooler *apiv1.Pooler) string {
	if pooler.Spec.Template == nil {
		return ""
	}
	for _, c := range pooler.Spec.Template.Spec.Containers {
		if c.Name == "pgbouncer" {
			return c.Image
		}
	}
	return ""
}

// isPgBouncerPaused reports whether the user has requested PgBouncer to pause
// new client connections via spec.pgbouncer.paused.
func isPgBouncerPaused(pooler *apiv1.Pooler) bool {
	return pooler.Spec.PgBouncer != nil &&
		pooler.Spec.PgBouncer.Paused != nil &&
		*pooler.Spec.PgBouncer.Paused
}

func (r *PoolerReconciler) resolveImageFromCatalog(ctx context.Context, pooler *apiv1.Pooler) (string, error) {
	ref := pooler.Spec.PgBouncer.ImageCatalogRef

	var catalog apiv1.GenericImageCatalog
	switch ref.Kind {
	case apiv1.ClusterImageCatalogKind:
		catalog = &apiv1.ClusterImageCatalog{}
	case apiv1.ImageCatalogKind:
		catalog = &apiv1.ImageCatalog{}
	default:
		return "", fmt.Errorf("invalid image catalog kind: %s", ref.Kind)
	}

	namespace := ""
	if ref.Kind == apiv1.ImageCatalogKind {
		namespace = pooler.Namespace
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, catalog); err != nil {
		if apierrs.IsNotFound(err) {
			return "", fmt.Errorf("%s %q not found", ref.Kind, ref.Name)
		}
		return "", fmt.Errorf("error getting %s %q: %w", ref.Kind, ref.Name, err)
	}

	image, ok := catalog.GetSpec().FindExtraImageForKey(ref.Key)
	if !ok {
		return "", fmt.Errorf("key %q not found in %s %q", ref.Key, ref.Kind, ref.Name)
	}
	return image, nil
}

func (r *PoolerReconciler) mapImageCatalogToPoolers() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		catalog, ok := obj.(*apiv1.ImageCatalog)
		if !ok {
			return nil
		}

		poolers, err := r.getPoolersForImageCatalog(ctx, catalog)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting pooler list", "namespace", catalog.Namespace)
			return nil
		}

		requests := make([]reconcile.Request, len(poolers.Items))
		for i, pooler := range poolers.Items {
			requests[i] = reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				},
			}
		}
		return requests
	}
}

func (r *PoolerReconciler) getPoolersForImageCatalog(
	ctx context.Context,
	catalog *apiv1.ImageCatalog,
) (apiv1.PoolerList, error) {
	var poolers apiv1.PoolerList
	err := r.List(ctx, &poolers, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(poolerImageCatalogKey, catalog.GetName()),
		Namespace:     catalog.GetNamespace(),
	})
	return poolers, err
}

func (r *PoolerReconciler) mapClusterImageCatalogToPoolers() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		catalog, ok := obj.(*apiv1.ClusterImageCatalog)
		if !ok {
			return nil
		}

		poolers, err := r.getPoolersForClusterImageCatalog(ctx, catalog)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting pooler list")
			return nil
		}

		requests := make([]reconcile.Request, len(poolers.Items))
		for i, pooler := range poolers.Items {
			requests[i] = reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				},
			}
		}
		return requests
	}
}

func (r *PoolerReconciler) getPoolersForClusterImageCatalog(
	ctx context.Context,
	catalog *apiv1.ClusterImageCatalog,
) (apiv1.PoolerList, error) {
	var poolers apiv1.PoolerList
	err := r.List(ctx, &poolers, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(poolerImageCatalogKey, catalog.GetName()),
	})
	return poolers, err
}
