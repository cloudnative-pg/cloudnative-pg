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

// This method prune the Database CRD finalizers that are orphans.
func (r *ClusterReconciler) deleteDatabaseFinalizers(ctx context.Context, namespacedName types.NamespacedName) error {
	databases := apiv1.DatabaseList{}
	err := r.List(
		ctx,
		&databases,
		client.InNamespace(namespacedName.Namespace),
	)
	if err != nil {
		return err
	}

	contextLogger := log.FromContext(ctx)

	for idx := range databases.Items {
		database := &databases.Items[idx]

		if database.Spec.ClusterRef.Name != namespacedName.Name {
			continue
		}

		// Database CRD name and cluster name match.
		// Check if the finalizer is still there, otherwise patch the Database CRD
		dbWithoutFinalizer := database.DeepCopy()
		if controllerutil.RemoveFinalizer(dbWithoutFinalizer, utils.DatabaseFinalizerName) {
			contextLogger.Info("Removing finalizer from database",
				"finalizer", utils.DatabaseFinalizerName, "database", database.Name)
			if err := r.Patch(ctx, dbWithoutFinalizer, client.MergeFrom(database)); err != nil {
				contextLogger.Error(
					err,
					"error while removing finalizer from database",
					"database", database.Name,
					"oldFinalizerList", database.ObjectMeta.Finalizers,
					"newFinalizerList", dbWithoutFinalizer.ObjectMeta.Finalizers,
				)
			}
		}
	}

	return nil
}
