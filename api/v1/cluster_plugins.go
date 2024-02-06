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
)

// LoadPlugin creates a new plugin client, loading the plugins that are required
// by this cluster
func (cluster *Cluster) LoadPlugin(ctx context.Context) (client.Client, error) {
	pluginLoader := client.NewUnixSocketClient(configuration.Current.PluginSocketDir)

	// Load the plugins
	for _, pluginDeclaration := range cluster.Spec.Plugins {
		if err := pluginLoader.Load(ctx, pluginDeclaration.Name); err != nil {
			return nil, err
		}
	}

	return pluginLoader, nil
}
