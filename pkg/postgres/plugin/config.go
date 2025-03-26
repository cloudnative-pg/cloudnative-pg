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

package plugin

import (
	"context"

	postgresClient "github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cnpgiClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	contextutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils/context"
)

// CreatePostgresqlConfigurationWithPlugins creates a new PostgreSQL configuration and enriches it by invoking
// the registered Plugins
func CreatePostgresqlConfigurationWithPlugins(
	ctx context.Context,
	info postgres.ConfigurationInfo,
	operationType postgresClient.OperationType_Type,
) (*postgres.PgConfiguration, error) {
	contextLogger := log.FromContext(ctx).WithName("enrichConfigurationWithPlugins")

	pgConf := postgres.CreatePostgresqlConfiguration(info)

	cluster, ok := ctx.Value(contextutils.ContextKeyCluster).(client.Object)
	if !ok || cluster == nil {
		contextLogger.Trace("skipping CreatePostgresqlConfigurationWithPlugins, cannot find the cluster inside the context")
		return pgConf, nil
	}

	pluginClient := cnpgiClient.GetPluginClientFromContext(ctx)
	if pluginClient == nil {
		contextLogger.Trace(
			"skipping CreatePostgresqlConfigurationWithPlugins, cannot find the plugin client inside the context")
		return pgConf, nil
	}

	conf, err := pluginClient.EnrichConfiguration(
		ctx,
		cluster,
		pgConf.GetConfigurationParameters(),
		operationType,
	)
	if err != nil {
		contextLogger.Error(err, "failed to enrich configuration with plugins")
		return nil, err
	}

	pgConf.SetConfigurationParameters(conf)

	return pgConf, nil
}
