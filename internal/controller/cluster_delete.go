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

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
)

// deleteDanglingMonitoringQueries deletes the default monitoring configMap and/or secret if no cluster in the namespace
// is using it.
func (r *ClusterReconciler) deleteDanglingMonitoringQueries(ctx context.Context, namespace string) error {
	configMapName := configuration.Current.MonitoringQueriesConfigmap
	secretName := configuration.Current.MonitoringQueriesSecret
	if secretName == "" && configMapName == "" {
		// no configmap or secretName configured, we can exit.
		return nil
	}

	// we avoid deleting the operator configmap.
	if namespace == configuration.Current.OperatorNamespace {
		return nil
	}

	clustersUsingDefaultMetrics := apiv1.ClusterList{}
	err := r.List(
		ctx,
		&clustersUsingDefaultMetrics,
		client.InNamespace(namespace),
		// we check if there are any clusters that use the configMap in the namespace
		client.MatchingFields{disableDefaultQueriesSpecPath: "false"},
	)
	if err != nil {
		return err
	}

	if len(clustersUsingDefaultMetrics.Items) > 0 {
		return nil
	}

	if configMapName != "" {
		configMap := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apiv1.DefaultMonitoringConfigMapName,
				Namespace: namespace,
			},
		}

		if err = r.Delete(ctx, &configMap); err != nil && !apierrs.IsNotFound(err) {
			return err
		}
	}

	if secretName != "" {
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apiv1.DefaultMonitoringSecretName,
				Namespace: namespace,
			},
		}

		if err = r.Delete(ctx, &secret); err != nil && !apierrs.IsNotFound(err) {
			return err
		}
	}

	return nil
}
