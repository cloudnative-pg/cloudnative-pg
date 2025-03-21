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

package plugin

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client/contracts"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// CreatePostgresqlConfiguration creates a new PostgreSQL configuration and enriches it by invoking
// the registered Plugins
func CreatePostgresqlConfiguration(
	ctx context.Context,
	info postgres.ConfigurationInfo,
) (*postgres.PgConfiguration, error) {
	return enrichConfigurationWithPlugins(ctx, postgres.CreatePostgresqlConfiguration(info))
}

func enrichConfigurationWithPlugins(
	ctx context.Context,
	baseConfig *postgres.PgConfiguration,
) (*postgres.PgConfiguration, error) {
	contextLogger := log.FromContext(ctx).WithName("enrichConfigurationWithPlugins")

	cluster, ok := ctx.Value(utils.ContextKeyCluster).(client.Object)
	if !ok || cluster == nil {
		contextLogger.Trace("skipping CreatePostgresqlConfiguration, cannot find the cluster inside the context")
		return baseConfig, nil
	}

	pluginClient, ok := ctx.Value(utils.PluginClientKey).(contracts.PostgresConfigurationCapabilities)
	if !ok || pluginClient == nil {
		contextLogger.Trace("skipping CreatePostgresqlConfiguration, cannot find the plugin client inside the context")
		return baseConfig, nil
	}

	enrichedMapConfig, err := pluginClient.EnrichConfiguration(ctx, cluster, baseConfig.GetConfigurationParameters())
	if err != nil {
		contextLogger.Error(err, "failed to enrich configuration with plugins")
		return nil, err
	}

	for key, value := range enrichedMapConfig {
		baseConfig.OverwriteConfig(key, value)
	}

	return baseConfig, nil
}
