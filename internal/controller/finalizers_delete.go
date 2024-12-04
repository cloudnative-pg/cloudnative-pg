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
	"k8s.io/apimachinery/pkg/api/meta"
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
	if err := r.notifyOwnedResourceDeletion(
		ctx,
		namespacedName,
		&apiv1.DatabaseList{},
		utils.DatabaseFinalizerName,
	); err != nil {
		return err
	}

	if err := r.notifyOwnedResourceDeletion(
		ctx,
		namespacedName,
		&apiv1.PublicationList{},
		utils.PublicationFinalizerName,
	); err != nil {
		return err
	}

	return r.notifyOwnedResourceDeletion(
		ctx,
		namespacedName,
		&apiv1.SubscriptionList{},
		utils.SubscriptionFinalizerName,
	)
}

// notifyOwnedResourceDeletion deletes finalizers for a given resource type
func (r *ClusterReconciler) notifyOwnedResourceDeletion(
	ctx context.Context,
	namespacedName types.NamespacedName,
	list client.ObjectList,
	finalizerName string,
) error {
	// TODO(armru): make this dependency more explicit
	// ClusterOwnedResourceWithStatus is a kubernetes resource object owned by a cluster that has status
	// capabilities
	type ClusterOwnedResourceWithStatus interface {
		client.Object
		GetClusterRef() corev1.LocalObjectReference
		GetStatusMessage() string
		SetAsFailed(err error)
	}

	contextLogger := log.FromContext(ctx)

	if err := r.List(ctx, list, client.InNamespace(namespacedName.Namespace)); err != nil {
		return err
	}

	items, err := meta.ExtractList(list)
	if err != nil {
		return err
	}

	for _, item := range items {
		obj, ok := item.(ClusterOwnedResourceWithStatus)
		if !ok {
			continue
		}

		itemLogger := contextLogger.WithValues(
			"resourceKind", obj.GetObjectKind().GroupVersionKind().Kind,
			"resourceName", obj.GetName(),
			"finalizerName", finalizerName,
		)
		if obj.GetClusterRef().Name != namespacedName.Name {
			continue
		}

		const statusMessage = "cluster resource has been deleted, skipping reconciliation"

		origObj := obj.DeepCopyObject().(ClusterOwnedResourceWithStatus)

		if obj.GetStatusMessage() != statusMessage {
			obj.SetAsFailed(errors.New(statusMessage))
			if err := r.Status().Patch(ctx, obj, client.MergeFrom(origObj)); err != nil {
				itemLogger.Error(err, "error while setting failed status for cluster deletion")
				return err
			}
		}

		if controllerutil.RemoveFinalizer(obj, finalizerName) {
			itemLogger.Debug("Removing finalizer from resource")
			if err := r.Patch(ctx, obj, client.MergeFrom(origObj)); err != nil {
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
