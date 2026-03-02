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

// Package imagecatalog provides utilities for fetching image catalogs
package imagecatalog

import (
	"context"
	"fmt"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Get retrieves the image catalog referenced by a cluster's ImageCatalogRef.
// The caller must ensure that cluster.Spec.ImageCatalogRef is not nil.
func Get(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
) (apiv1.GenericImageCatalog, error) {
	catalogKind := cluster.Spec.ImageCatalogRef.Kind
	var catalog apiv1.GenericImageCatalog

	switch catalogKind {
	case apiv1.ClusterImageCatalogKind:
		catalog = &apiv1.ClusterImageCatalog{}
	case apiv1.ImageCatalogKind:
		catalog = &apiv1.ImageCatalog{}
	default:
		return nil, fmt.Errorf("invalid image catalog type: %s", catalogKind)
	}

	apiGroup := cluster.Spec.ImageCatalogRef.APIGroup
	if apiGroup == nil || *apiGroup != apiv1.SchemeGroupVersion.Group {
		return nil, fmt.Errorf("invalid image catalog group")
	}

	catalogName := cluster.Spec.ImageCatalogRef.Name
	err := c.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: catalogName}, catalog)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, fmt.Errorf("catalog %s/%s not found", catalogKind, catalogName)
		}
		return nil, fmt.Errorf("error getting catalog: %w", err)
	}

	return catalog, nil
}
