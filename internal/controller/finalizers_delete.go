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
	"errors"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// notifyDeletionToOwnedResources notifies the cluster deletion to the managed owned resources
func (r *ClusterReconciler) notifyDeletionToOwnedResources(
	ctx context.Context,
	namespacedName types.NamespacedName,
) error {
	var dbList apiv1.DatabaseList
	if err := r.List(ctx, &dbList, client.InNamespace(namespacedName.Namespace)); err != nil {
		return err
	}

	if err := notifyOwnedResourceDeletion(
		ctx,
		r.Client,
		namespacedName,
		toSliceWithPointers(dbList.Items),
		utils.DatabaseFinalizerName,
	); err != nil {
		return err
	}

	var pbList apiv1.PublicationList
	if err := r.List(ctx, &pbList, client.InNamespace(namespacedName.Namespace)); err != nil {
		return err
	}

	if err := notifyOwnedResourceDeletion(
		ctx,
		r.Client,
		namespacedName,
		toSliceWithPointers(pbList.Items),
		utils.PublicationFinalizerName,
	); err != nil {
		return err
	}

	var sbList apiv1.SubscriptionList
	if err := r.List(ctx, &sbList, client.InNamespace(namespacedName.Namespace)); err != nil {
		return err
	}

	return notifyOwnedResourceDeletion(
		ctx,
		r.Client,
		namespacedName,
		toSliceWithPointers(sbList.Items),
		utils.SubscriptionFinalizerName,
	)
}

// clusterOwnedResourceWithStatus is a kubernetes resource object owned by a cluster that has status
// capabilities
type clusterOwnedResourceWithStatus interface {
	client.Object
	GetClusterRef() corev1.LocalObjectReference
	GetStatusMessage() string
	SetAsFailed(err error)
}

func toSliceWithPointers[T any](items []T) []*T {
	result := make([]*T, len(items))
	for i, item := range items {
		result[i] = &item
	}
	return result
}

// notifyOwnedResourceDeletion deletes finalizers for a given resource type
func notifyOwnedResourceDeletion[T clusterOwnedResourceWithStatus](
	ctx context.Context,
	cli client.Client,
	namespacedName types.NamespacedName,
	objects []T,
	finalizerName string,
) error {
	contextLogger := log.FromContext(ctx)
	for _, obj := range objects {
		itemLogger := contextLogger.WithValues(
			"resourceKind", obj.GetObjectKind().GroupVersionKind().Kind,
			"resourceName", obj.GetName(),
			"finalizerName", finalizerName,
		)
		if obj.GetClusterRef().Name != namespacedName.Name {
			continue
		}

		const statusMessage = "cluster resource has been deleted, skipping reconciliation"

		origObj := obj.DeepCopyObject().(T)

		if obj.GetStatusMessage() != statusMessage {
			obj.SetAsFailed(errors.New(statusMessage))
			if err := cli.Status().Patch(ctx, obj, client.MergeFrom(origObj)); err != nil {
				itemLogger.Error(err, "error while setting failed status for cluster deletion")
				return err
			}
		}

		if controllerutil.RemoveFinalizer(obj, finalizerName) {
			itemLogger.Debug("Removing finalizer from resource")
			if err := cli.Patch(ctx, obj, client.MergeFrom(origObj)); err != nil {
				itemLogger.Error(
					err,
					"while removing the finalizer",
					"oldFinalizerList", origObj.GetFinalizers(),
					"newFinalizerList", obj.GetFinalizers(),
				)
				return err
			}
		}
	}

	return nil
}
