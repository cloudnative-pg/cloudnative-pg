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
)

// reconcileImage processes the image request, executes it, and stores
// the result in the .status.image field. If the user requested a
// major version upgrade, the current image is saved in the
// .status.majorVersionUpgradeFromImage field. This allows for
// reverting the upgrade if it doesn't complete successfully.
func (r *ClusterReconciler) reconcileImage(ctx context.Context, cluster *apiv1.Cluster) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	image, err := r.getConfiguredImage(ctx, cluster)
	if err != nil {
		return &ctrl.Result{}, r.RegisterPhase(ctx, cluster, apiv1.PhaseImageCatalogError, err.Error())
	}

	currentDataImage := getCurrentPgDataImage(&cluster.Status)

	// Case 1: the cluster is being initialized and there is still no
	// running image. In this case, we should simply apply the image selected by the user.
	if currentDataImage == "" {
		return nil, status.PatchWithOptimisticLock(
			ctx,
			r.Client,
			cluster,
			status.SetImage(image),
			status.SetMajorVersionUpgradeFromImage(nil),
		)
	}

	// Case 2: there's a running image. The code checks if the user selected
	// an image of the same major version or if a change in the major
	// version has been requested.
	currentVersion, err := version.FromTag(reference.New(currentDataImage).Tag)
	if err != nil {
		contextLogger.Error(err, "While parsing current major versions")
		return nil, err
	}

	requestedVersion, err := version.FromTag(reference.New(image).Tag)
	if err != nil {
		contextLogger.Error(err, "While parsing requested major versions")
		return nil, err
	}

	var majorVersionUpgradeFromImage *string
	switch {
	case currentVersion.Major() < requestedVersion.Major():
		// The current major version is older than the requested one
		majorVersionUpgradeFromImage = &currentDataImage
	case currentVersion.Major() == requestedVersion.Major():
		// The major versions are the same, cancel the update
		majorVersionUpgradeFromImage = nil
	default:
		contextLogger.Info(
			"Cannot downgrade the PostgreSQL major version. Forcing the current image.",
			"currentImage", currentDataImage,
			"requestedImage", image)
		image = currentDataImage
	}

	return nil, status.PatchWithOptimisticLock(
		ctx,
		r.Client,
		cluster,
		status.SetImage(image),
		status.SetMajorVersionUpgradeFromImage(majorVersionUpgradeFromImage),
	)
}

// getCurrentPgDataImage returns Postgres image that was able to run the cluster
// PGDATA correctly last time.
// This is important in the context of major upgrade because it contains the
// image with the "old" major version even when there are no Pods available.
func getCurrentPgDataImage(status *apiv1.ClusterStatus) string {
	if status.MajorVersionUpgradeFromImage != nil {
		return *status.MajorVersionUpgradeFromImage
	}

	return status.Image
}

func (r *ClusterReconciler) getConfiguredImage(ctx context.Context, cluster *apiv1.Cluster) (string, error) {
	contextLogger := log.FromContext(ctx)

	// If ImageName is defined and different from the current image in the status, we update the status
	if cluster.Spec.ImageName != "" {
		return cluster.Spec.ImageName, nil
	}

	if cluster.Spec.ImageCatalogRef == nil {
		return "", fmt.Errorf("ImageName is not defined and no catalog is referenced")
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
		return "", fmt.Errorf("invalid image catalog type")
	}

	apiGroup := cluster.Spec.ImageCatalogRef.APIGroup
	if apiGroup == nil || *apiGroup != apiv1.SchemeGroupVersion.Group {
		contextLogger.Info("Unknown catalog group")
		return "", fmt.Errorf("invalid image catalog group")
	}

	// Get the referenced catalog
	catalogName := cluster.Spec.ImageCatalogRef.Name
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: catalogName}, catalog)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(cluster, "Warning", "DiscoverImage", "Cannot get %v/%v",
				catalogKind, catalogName)
			contextLogger.Info("catalog not found", "catalogKind", catalogKind, "catalogName", catalogName)
			return "", fmt.Errorf("catalog %s/%s not found", catalogKind, catalogName)
		}

		return "", err
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
		return "", fmt.Errorf("selected major version is not available in the catalog")
	}

	return catalogImage, nil
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
		FieldSelector: fields.OneTermEqualSelector(".spec.imageCatalog.name", object.GetName()),
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
		FieldSelector: fields.OneTermEqualSelector(".spec.imageCatalog.name", object.GetName()),
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
