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

package controllers

import (
	"context"
	"fmt"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// reconcileImage sets the image inside the status, to be used by the following
// functions of the reconciler loop
func (r *ClusterReconciler) reconcileImage(ctx context.Context, cluster *apiv1.Cluster) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	oldCluster := cluster.DeepCopy()

	// If ImageName is defined and different from the current image in the status, we update the status
	if cluster.Spec.ImageName != "" && cluster.Status.Image != cluster.Spec.ImageName {
		cluster.Status.Image = cluster.Spec.ImageName
		if err := r.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
			contextLogger.Error(
				err,
				"While patching cluster status to set the image name from the cluster Spec",
				"imageName", cluster.Status.Image,
			)
			return nil, err
		}
		return nil, nil
	}

	// If ImageName was defined, we rely on what the user requested
	if cluster.Spec.ImageCatalogRef == nil {
		return nil, nil
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
		return &ctrl.Result{}, r.RegisterPhase(ctx, cluster, apiv1.PhaseImageCatalogError,
			"Invalid image catalog type")
	}

	apiGroup := cluster.Spec.ImageCatalogRef.APIGroup
	if apiGroup == nil || *apiGroup != apiv1.GroupVersion.Group {
		contextLogger.Info("Unknown catalog group")
		return &ctrl.Result{}, r.RegisterPhase(ctx, cluster, apiv1.PhaseImageCatalogError,
			"Invalid image catalog group")
	}

	// Get the referenced catalog
	catalogName := cluster.Spec.ImageCatalogRef.Name
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: catalogName}, catalog)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(cluster, "Warning", "DiscoverImage", "Cannot get %v/%v",
				catalogKind, catalogName)
			return &ctrl.Result{}, nil
		}

		return nil, err
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
		return &ctrl.Result{}, r.RegisterPhase(ctx, cluster, apiv1.PhaseImageCatalogError,
			"Selected major version is not available in the catalog")
	}

	// If the image is different, we set it into the cluster status
	if cluster.Spec.ImageName != catalogImage {
		cluster.Status.Image = catalogImage
		patch := client.MergeFrom(oldCluster)
		if err := r.Status().Patch(ctx, cluster, patch); err != nil {
			patchBytes, _ := patch.Data(cluster)
			contextLogger.Error(
				err,
				"While patching cluster status to set the image name from the catalog",
				"patch", string(patchBytes))
			return nil, err
		}
	}

	return nil, nil
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
