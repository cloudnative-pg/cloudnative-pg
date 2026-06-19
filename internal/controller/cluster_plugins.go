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

// Package controller contains the controller of the CRD
package controller

import (
	"context"
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiclient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// updatePluginsStatus ensures that we load the plugins that are required to reconcile
// this cluster
func (r *ClusterReconciler) updatePluginsStatus(ctx context.Context, cluster *apiv1.Cluster) error {
	// Load the plugins
	pluginClient := cnpgiclient.GetPluginClientFromContext(ctx)

	// Get the status of the plugins and store it inside the status section
	oldCluster := cluster.DeepCopy()
	metadataList := pluginClient.MetadataList()
	cluster.Status.PluginStatus = make([]apiv1.PluginStatus, len(metadataList))
	for i, entry := range metadataList {
		cluster.Status.PluginStatus[i].Name = entry.Name
		cluster.Status.PluginStatus[i].Version = entry.Version
		cluster.Status.PluginStatus[i].Capabilities = entry.Capabilities
		cluster.Status.PluginStatus[i].OperatorCapabilities = entry.OperatorCapabilities
		cluster.Status.PluginStatus[i].WALCapabilities = entry.WALCapabilities
		cluster.Status.PluginStatus[i].BackupCapabilities = entry.BackupCapabilities
		cluster.Status.PluginStatus[i].RestoreJobHookCapabilities = entry.RestoreJobHookCapabilities
	}

	// If nothing changes, there's no need to hit the API server
	if reflect.DeepEqual(oldCluster.Status.PluginStatus, cluster.Status.PluginStatus) {
		return nil
	}

	return r.Client.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster))
}

// mapPluginEndpointSlicesToClusters enqueues a reconcile request for every
// cluster that uses a plugin whose backing Service has its endpoints changing.
// EndpointSlice events fire when Pods behind the Service become Ready or
// NotReady, so the operator picks up plugin rollouts after the new Pods are
// actually serving traffic.
func (r *ClusterReconciler) mapPluginEndpointSlicesToClusters(
	operatorNamespace string,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		logger := log.FromContext(ctx)

		slice, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			return nil
		}

		serviceName := slice.Labels[discoveryv1.LabelServiceName]
		if serviceName == "" {
			return nil
		}

		var service corev1.Service
		err := r.Get(
			ctx,
			types.NamespacedName{Namespace: slice.Namespace, Name: serviceName},
			&service,
		)
		if apierrs.IsNotFound(err) {
			return nil
		}
		if err != nil {
			logger.Error(
				err,
				"Error while resolving the Service owning a plugin EndpointSlice, skipping",
				"endpointSlice", client.ObjectKeyFromObject(slice),
				"serviceName", serviceName,
			)
			return nil
		}

		if !isPluginService(&service, operatorNamespace) {
			return nil
		}

		pluginName := service.Labels[utils.PluginNameLabelName]
		if pluginName == "" {
			return nil
		}

		var clusterList apiv1.ClusterList
		if err := r.List(
			ctx,
			&clusterList,
			client.MatchingFields{usedPluginsClusterKey: pluginName},
		); err != nil {
			logger.Error(
				err,
				"Error while looking up clusters using a plugin whose endpoints changed, skipping",
				"endpointSlice", client.ObjectKeyFromObject(slice),
				"pluginName", pluginName,
			)
			return nil
		}

		result := make([]reconcile.Request, len(clusterList.Items))
		for i := range clusterList.Items {
			result[i].Name = clusterList.Items[i].Name
			result[i].Namespace = clusterList.Items[i].Namespace
		}
		return result
	}
}
