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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// deleteFinalizers deletes object finalizers when the cluster they were in has been deleted
func (r *ClusterReconciler) deleteFinalizers(ctx context.Context, namespacedName types.NamespacedName) error {
	if err := r.deleteDatabaseFinalizers(ctx, namespacedName); err != nil {
		return err
	}
	if err := r.deletePublicationFinalizers(ctx, namespacedName); err != nil {
		return err
	}
	if err := r.deleteSubscriptionFinalizers(ctx, namespacedName); err != nil {
		return err
	}
	return nil
}

// deleteDatabaseFinalizers deletes Database object finalizers when the cluster they were in has been deleted
// nolint: dupl
func (r *ClusterReconciler) deleteDatabaseFinalizers(
	ctx context.Context,
	namespacedName types.NamespacedName,
) error {
	contextLogger := log.FromContext(ctx)

	databases := apiv1.DatabaseList{}
	if err := r.List(ctx,
		&databases,
		client.InNamespace(namespacedName.Namespace),
	); err != nil {
		return err
	}

	for idx := range databases.Items {
		database := &databases.Items[idx]

		if database.Spec.ClusterRef.Name != namespacedName.Name {
			continue
		}

		origDatabase := database.DeepCopy()
		if controllerutil.RemoveFinalizer(database, utils.DatabaseFinalizerName) {
			contextLogger.Debug("Removing finalizer from database",
				"finalizer", utils.DatabaseFinalizerName, "database", database.Name)
			if err := r.Patch(ctx, database, client.MergeFrom(origDatabase)); err != nil {
				contextLogger.Error(
					err,
					"error while removing finalizer from database",
					"database", database.Name,
					"oldFinalizerList", origDatabase.ObjectMeta.Finalizers,
					"newFinalizerList", database.ObjectMeta.Finalizers,
				)
				return err
			}
		}
	}

	return nil
}

// deletePublicationFinalizers deletes Publication object finalizers when the cluster they were in has been deleted
// nolint: dupl
func (r *ClusterReconciler) deletePublicationFinalizers(
	ctx context.Context,
	namespacedName types.NamespacedName,
) error {
	contextLogger := log.FromContext(ctx)

	publications := apiv1.PublicationList{}
	if err := r.List(ctx,
		&publications,
		client.InNamespace(namespacedName.Namespace),
	); err != nil {
		return err
	}

	for idx := range publications.Items {
		publication := &publications.Items[idx]

		if publication.Spec.ClusterRef.Name != namespacedName.Name {
			continue
		}

		origPublication := publication.DeepCopy()
		if controllerutil.RemoveFinalizer(publication, utils.PublicationFinalizerName) {
			contextLogger.Debug("Removing finalizer from publication",
				"finalizer", utils.PublicationFinalizerName, "publication", publication.Name)
			if err := r.Patch(ctx, publication, client.MergeFrom(origPublication)); err != nil {
				contextLogger.Error(
					err,
					"error while removing finalizer from publication",
					"publication", publication.Name,
					"oldFinalizerList", origPublication.ObjectMeta.Finalizers,
					"newFinalizerList", publication.ObjectMeta.Finalizers,
				)
				return err
			}
		}
	}

	return nil
}

// deleteSubscriptionFinalizers deletes Subscription object finalizers when the cluster they were in has been deleted
// nolint: dupl
func (r *ClusterReconciler) deleteSubscriptionFinalizers(
	ctx context.Context,
	namespacedName types.NamespacedName,
) error {
	contextLogger := log.FromContext(ctx)

	subscriptions := apiv1.SubscriptionList{}
	if err := r.List(ctx,
		&subscriptions,
		client.InNamespace(namespacedName.Namespace),
	); err != nil {
		return err
	}

	for idx := range subscriptions.Items {
		subscription := &subscriptions.Items[idx]

		if subscription.Spec.ClusterRef.Name != namespacedName.Name {
			continue
		}

		origSubscription := subscription.DeepCopy()
		if controllerutil.RemoveFinalizer(subscription, utils.SubscriptionFinalizerName) {
			contextLogger.Debug("Removing finalizer from subscription",
				"finalizer", utils.SubscriptionFinalizerName, "subscription", subscription.Name)
			if err := r.Patch(ctx, subscription, client.MergeFrom(origSubscription)); err != nil {
				contextLogger.Error(
					err,
					"error while removing finalizer from subscription",
					"subscription", subscription.Name,
					"oldFinalizerList", origSubscription.ObjectMeta.Finalizers,
					"newFinalizerList", subscription.ObjectMeta.Finalizers,
				)
				return err
			}
		}
	}

	return nil
}
