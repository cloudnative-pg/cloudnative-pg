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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// setCrdAsNotReconciled patches the status of the objects as not reconciled,
// when the cluster they were in has been deleted
func (r *ClusterReconciler) setCrdAsNotReconciled(ctx context.Context, namespacedName types.NamespacedName) error {
	if err := r.setCrdAsNotReconciledForResource(
		ctx,
		namespacedName,
		&apiv1.DatabaseList{},
	); err != nil {
		return err
	}

	if err := r.setCrdAsNotReconciledForResource(
		ctx,
		namespacedName,
		&apiv1.PublicationList{},
	); err != nil {
		return err
	}

	return r.setCrdAsNotReconciledForResource(
		ctx,
		namespacedName,
		&apiv1.SubscriptionList{},
	)
}

// setCrdAsNotReconciledForResource patches the status of the objects as not reconciled for a given resource type
// nolint: gocognit
func (r *ClusterReconciler) setCrdAsNotReconciledForResource(
	ctx context.Context,
	namespacedName types.NamespacedName,
	list client.ObjectList,
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

		origDatabaseObj, ok := obj.DeepCopyObject().(*apiv1.Database)
		if ok {
			origDatabaseObj.Status.Applied = ptr.To(false)
			origDatabaseObj.Status.Message = "The orphan database object is not reconciled"
			contextLogger.Debug("Set database as not reconciled", "resource", obj.GetName())
			if err := r.Status().Patch(ctx, origDatabaseObj, client.MergeFrom(obj)); err != nil {
				contextLogger.Error(
					err,
					"error while setting database as not reconciled",
					"resource", obj.GetName(),
					"kind", obj.GetObjectKind().GroupVersionKind().Kind,
				)
				return err
			}
			continue
		}

		origPubObj, ok := obj.DeepCopyObject().(*apiv1.Publication)
		if ok {
			origPubObj.Status.Applied = ptr.To(false)
			origPubObj.Status.Message = "The orphan publication object is not reconciled"
			contextLogger.Debug("Set publication as not reconciled", "resource", obj.GetName())
			if err := r.Status().Patch(ctx, origPubObj, client.MergeFrom(obj)); err != nil {
				contextLogger.Error(
					err,
					"error while setting publication as not reconciled",
					"resource", obj.GetName(),
					"kind", obj.GetObjectKind().GroupVersionKind().Kind,
				)
				return err
			}
			continue
		}

		origSubObj, ok := obj.DeepCopyObject().(*apiv1.Subscription)
		if ok {
			origSubObj.Status.Applied = ptr.To(false)
			origSubObj.Status.Message = "The orphan subscription object is not reconciled"
			contextLogger.Debug("Set subscription as not reconciled", "resource", obj.GetName())
			if err := r.Status().Patch(ctx, origSubObj, client.MergeFrom(obj)); err != nil {
				contextLogger.Error(
					err,
					"error while setting subscription as not reconciled",
					"resource", obj.GetName(),
					"kind", obj.GetObjectKind().GroupVersionKind().Kind,
				)
				return err
			}
			continue
		}
	}

	return nil
}
