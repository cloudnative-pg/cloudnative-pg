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

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiclient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
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
