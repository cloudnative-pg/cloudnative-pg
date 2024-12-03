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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ClusterReferrer is an object containing a cluster reference
type ClusterReferrer interface {
	GetClusterRef() corev1.LocalObjectReference
	client.Object
}

// StatusSetter is an object that can set a status
type StatusSetter interface {
	SetAsFailed(err error)
	client.Object
}

// deleteFinalizers deletes object finalizers when the cluster they were in has been deleted
func (r *ClusterReconciler) deleteFinalizers(ctx context.Context, namespacedName types.NamespacedName) error {
	if err := r.deleteFinalizersForResource(
		ctx,
		namespacedName,
		&apiv1.DatabaseList{},
		utils.DatabaseFinalizerName,
	); err != nil {
		return err
	}

	if err := r.deleteFinalizersForResource(
		ctx,
		namespacedName,
		&apiv1.PublicationList{},
		utils.PublicationFinalizerName,
	); err != nil {
		return err
	}

	return r.deleteFinalizersForResource(
		ctx,
		namespacedName,
		&apiv1.SubscriptionList{},
		utils.SubscriptionFinalizerName,
	)
}

// deleteFinalizersForResource deletes finalizers for a given resource type
func (r *ClusterReconciler) deleteFinalizersForResource(
	ctx context.Context,
	namespacedName types.NamespacedName,
	list client.ObjectList,
	finalizerName string,
) error {
	contextLogger := log.FromContext(ctx)

	if err := r.List(ctx, list, client.InNamespace(namespacedName.Namespace)); err != nil {
		return err
	}

	items, err := meta.ExtractList(list)
	if err != nil {
		return err
	}

	for _, item := range items {
		obj, ok := item.(ClusterReferrer)
		if !ok {
			continue
		}

		if obj.GetClusterRef().Name != namespacedName.Name {
			continue
		}

		origObj := obj.DeepCopyObject().(ClusterReferrer)
		if controllerutil.RemoveFinalizer(obj, finalizerName) {
			contextLogger.Debug("Removing finalizer from resource",
				"finalizer", finalizerName, "resource", obj.GetName())

			if err := r.Patch(ctx, obj, client.MergeFrom(origObj)); err != nil {
				contextLogger.Error(
					err,
					"error while removing finalizer from resource",
					"resource", obj.GetName(),
					"kind", obj.GetObjectKind().GroupVersionKind().Kind,
					"oldFinalizerList", origObj.GetFinalizers(),
					"newFinalizerList", obj.GetFinalizers(),
				)
				return err
			}

			// set status as failed, as the orphan resource is not reconciled
			obj.(StatusSetter).SetAsFailed(fmt.Errorf("orphan resource is not reconciled"))
			if err := r.Status().Patch(ctx, obj, client.MergeFrom(origObj)); err != nil {
				contextLogger.Error(
					err,
					"error while patching resource status",
					"resource", obj.GetName(),
					"kind", obj.GetObjectKind().GroupVersionKind().Kind,
				)
				return err
			}
		}
	}

	return nil
}
