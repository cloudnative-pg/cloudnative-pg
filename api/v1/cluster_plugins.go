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

package v1

import (
	"context"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetPluginNames gets the name of the plugins that are involved
// in the reconciliation of this cluster
func (cluster *Cluster) GetPluginNames() (result []string) {
	pluginNames := make([]string, len(cluster.Spec.Plugins))
	for i, pluginDeclaration := range cluster.Spec.Plugins {
		pluginNames[i] = pluginDeclaration.Name
	}
	return pluginNames
}

// GetWALPluginNames gets the list of all the plugin names capable of handling
// the WAL service
func (cluster *Cluster) GetWALPluginNames() (result []string) {
	result = make([]string, 0, len(cluster.Status.PluginStatus))
	for _, entry := range cluster.Status.PluginStatus {
		if len(entry.WALCapabilities) > 0 {
			result = append(result, entry.Name)
		}
	}

	return result
}

// SetInContext records the cluster in the given context
func (cluster *Cluster) SetInContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, utils.ContextKeyCluster, cluster)
}
