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

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// LoadPluginClient creates a new plugin client, loading the plugins that are
// required by this cluster
func (cluster *Cluster) LoadPluginClient(ctx context.Context) (client.Client, error) {
	pluginNames := make([]string, len(cluster.Spec.Plugins))
	for i, pluginDeclaration := range cluster.Spec.Plugins {
		pluginNames[i] = pluginDeclaration.Name
	}

	return cluster.LoadSelectedPluginsClient(ctx, pluginNames)
}

// LoadSelectedPluginsClient creates a new plugin client, loading the requested
// plugins
func (cluster *Cluster) LoadSelectedPluginsClient(ctx context.Context, pluginNames []string) (client.Client, error) {
	pluginLoader := client.NewUnixSocketClient(configuration.Current.PluginSocketDir)

	// Load the plugins
	for _, name := range pluginNames {
		if err := pluginLoader.Load(ctx, name); err != nil {
			return nil, err
		}
	}

	return pluginLoader, nil
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
