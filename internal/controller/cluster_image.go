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
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/extensions"
)

// reconcileImage processes the image request, executes it, and stores
// the result in the .status.image field. If the user requested a
// major version upgrade, the current image is saved in the
// .status.pgDataImageInfo field.
func (r *ClusterReconciler) reconcileImage(ctx context.Context, cluster *apiv1.Cluster) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	requestedImageInfo, err := r.getRequestedImageInfo(ctx, cluster)
	if err != nil {
		return &ctrl.Result{}, r.RegisterPhase(ctx, cluster, apiv1.PhaseImageCatalogError, err.Error())
	}

	// Case 1: the cluster is being initialized and there is still no
	// running image. In this case, we should simply apply the image selected by the user.
	if cluster.Status.PGDataImageInfo == nil {
		return nil, status.PatchWithOptimisticLock(
			ctx,
			r.Client,
			cluster,
			status.SetImage(requestedImageInfo.Image),
			status.SetPGDataImageInfo(&requestedImageInfo),
		)
	}

	extensionsChanged := !reflect.DeepEqual(cluster.Status.PGDataImageInfo.Extensions, requestedImageInfo.Extensions)
	imageChanged := requestedImageInfo.Image != cluster.Status.PGDataImageInfo.Image

	currentMajorVersion := cluster.Status.PGDataImageInfo.MajorVersion
	requestedMajorVersion := requestedImageInfo.MajorVersion

	// Case 2: nothing to be done.
	if !imageChanged && !extensionsChanged {
		return nil, nil
	}

	// Case 3: there's a running image. The code checks if the user selected
	// an image of the same major version or if a change in the major
	// version has been requested.
	if imageChanged {
		if currentMajorVersion > requestedMajorVersion {
			// Major version downgrade requested. This is not allowed.
			contextLogger.Info(
				"Cannot downgrade the PostgreSQL major version. Forcing the current requestedImageInfo.",
				"currentImage", cluster.Status.PGDataImageInfo.Image,
				"requestedImage", requestedImageInfo)
			return nil, fmt.Errorf("cannot downgrade the PostgreSQL major version from %d to %d",
				currentMajorVersion, requestedMajorVersion)
		}

		if currentMajorVersion < requestedMajorVersion {
			// Major version upgrade requested
			return nil, status.PatchWithOptimisticLock(
				ctx,
				r.Client,
				cluster,
				status.SetImage(requestedImageInfo.Image),
			)
		}
	}

	// Case 4: This is either a minor version upgrade/downgrade or a
	// change to the extension images.
	return nil, status.PatchWithOptimisticLock(
		ctx,
		r.Client,
		cluster,
		status.SetImage(requestedImageInfo.Image),
		status.SetPGDataImageInfo(&requestedImageInfo))
}

func getImageInfoFromImage(cluster *apiv1.Cluster) (apiv1.ImageInfo, error) {
	// Parse the version from the tag
	imageVersion, err := version.FromTag(reference.New(cluster.Spec.ImageName).Tag)
	if err != nil {
		return apiv1.ImageInfo{},
			fmt.Errorf("cannot parse version from image %s: %w", cluster.Spec.ImageName, err)
	}

	return apiv1.ImageInfo{
		Image:        cluster.Spec.ImageName,
		MajorVersion: int(imageVersion.Major()), //nolint:gosec
		Extensions:   cluster.Spec.PostgresConfiguration.Extensions,
	}, nil
}

func (r *ClusterReconciler) getRequestedImageInfo(
	ctx context.Context, cluster *apiv1.Cluster,
) (apiv1.ImageInfo, error) {
	contextLogger := log.FromContext(ctx)

	if cluster.Spec.ImageCatalogRef == nil {
		if cluster.Spec.ImageName != "" {
			return getImageInfoFromImage(cluster)
		}

		return apiv1.ImageInfo{}, fmt.Errorf("ImageName is not defined and no catalog is referenced")
	}

	contextLogger = contextLogger.WithValues("catalogRef", cluster.Spec.ImageCatalogRef)

	// Ensure the catalog has a correct type
	catalogKind := cluster.Spec.ImageCatalogRef.Kind
	var catalog apiv1.GenericImageCatalog
	switch catalogKind {
	case apiv1.ClusterImageCatalogKind:
		catalog = &apiv1.ClusterImageCatalog{}
	case apiv1.ImageCatalogKind:
		catalog = &apiv1.ImageCatalog{}
	default:
		contextLogger.Info("Unknown catalog kind")
		return apiv1.ImageInfo{}, fmt.Errorf("invalid image catalog type")
	}

	apiGroup := cluster.Spec.ImageCatalogRef.APIGroup
	if apiGroup == nil || *apiGroup != apiv1.SchemeGroupVersion.Group {
		contextLogger.Info("Unknown catalog group")
		return apiv1.ImageInfo{}, fmt.Errorf("invalid image catalog group")
	}

	// Get the referenced catalog
	catalogName := cluster.Spec.ImageCatalogRef.Name
	err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: catalogName}, catalog)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(cluster, "Warning", "DiscoverImage", "Cannot get %v/%v",
				catalogKind, catalogName)
			contextLogger.Info("catalog not found", "catalogKind", catalogKind, "catalogName", catalogName)
			return apiv1.ImageInfo{}, fmt.Errorf("catalog %s/%s not found", catalogKind, catalogName)
		}

		r.Recorder.Eventf(cluster, "Warning", "DiscoverImage", "Error getting %v/%v: %v",
			catalogKind, catalogName, err)
		contextLogger.Error(err, "while getting imageCatalog",
			"catalogKind", catalogKind, "catalogName", catalogName)
		return apiv1.ImageInfo{}, err
	}

	// Catalog found, we try to find the image for the major version
	requestedMajorVersion := cluster.Spec.ImageCatalogRef.Major
	catalogImage, ok := catalog.GetSpec().FindImageForMajor(requestedMajorVersion)
	if !ok {
		r.Recorder.Eventf(
			cluster,
			"Warning",
			"DiscoverImage", "Cannot find major %v in %v/%v",
			cluster.Spec.ImageCatalogRef.Major,
			catalogKind,
			catalogName)
		contextLogger.Info("cannot find requested major version",
			"requestedMajorVersion", requestedMajorVersion)
		return apiv1.ImageInfo{}, fmt.Errorf("selected major version is not available in the catalog")
	}

	exts, err := extensions.ResolveFromCatalog(cluster, catalog, requestedMajorVersion)
	if err != nil {
		return apiv1.ImageInfo{}, fmt.Errorf("cannot retrieve extensions for image %s: %w", catalogImage, err)
	}

	return apiv1.ImageInfo{
		Image:        catalogImage,
		MajorVersion: requestedMajorVersion,
		Extensions:   exts,
	}, nil
}

func (r *ClusterReconciler) getClustersForImageCatalogsToClustersMapper(
	ctx context.Context,
	object metav1.Object,
) (clusters apiv1.ClusterList, err error) {
	_, isCatalog := object.(*apiv1.ImageCatalog)
	if !isCatalog {
		return clusters, fmt.Errorf("unsupported object: %+v", object)
	}

	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(imageCatalogKey, object.GetName()),
		Namespace:     object.GetNamespace(),
	}

	err = r.List(ctx, &clusters, listOps)

	return clusters, err
}

func (r *ClusterReconciler) mapClusterImageCatalogsToClusters() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		catalog, ok := obj.(*apiv1.ClusterImageCatalog)
		if !ok {
			return nil
		}
		clusters, err := r.getClustersForClusterImageCatalogsToClustersMapper(ctx, catalog)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting cluster list")
			return nil
		}

		var requests []reconcile.Request
		for _, cluster := range clusters.Items {
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      cluster.Name,
						Namespace: cluster.Namespace,
					},
				},
			)
		}
		return requests
	}
}

func (r *ClusterReconciler) getClustersForClusterImageCatalogsToClustersMapper(
	ctx context.Context,
	object metav1.Object,
) (clusters apiv1.ClusterList, err error) {
	_, isCatalog := object.(*apiv1.ClusterImageCatalog)

	if !isCatalog {
		return clusters, fmt.Errorf("unsupported object: %+v", object)
	}

	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(imageCatalogKey, object.GetName()),
	}

	err = r.List(ctx, &clusters, listOps)

	return clusters, err
}

func (r *ClusterReconciler) mapImageCatalogsToClusters() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		catalog, ok := obj.(*apiv1.ImageCatalog)
		if !ok {
			return nil
		}
		clusters, err := r.getClustersForImageCatalogsToClustersMapper(ctx, catalog)
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting cluster list", "namespace", catalog.Namespace)
			return nil
		}

		var requests []reconcile.Request
		for _, cluster := range clusters.Items {
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      cluster.Name,
						Namespace: cluster.Namespace,
					},
				},
			)
		}
		return requests
	}
}
