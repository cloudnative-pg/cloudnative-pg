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

package client

import (
	"context"
	"encoding/json"
	"slices"

	postgresClient "github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PostgresConfigurationCapabilities is the interface that defines the
// capabilities of interacting with PostgreSQL.
type PostgresConfigurationCapabilities interface {
	// EnrichConfiguration is the method that enriches the PostgreSQL configuration
	EnrichConfiguration(
		ctx context.Context,
		cluster client.Object,
		config map[string]string,
		operationType postgresClient.OperationType_Type,
	) (map[string]string, error)
}

func (data *data) EnrichConfiguration(
	ctx context.Context,
	cluster client.Object,
	config map[string]string,
	operationType postgresClient.OperationType_Type,
) (map[string]string, error) {
	m, err := data.innerEnrichConfiguration(ctx, cluster, config, operationType)
	return m, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerEnrichConfiguration(
	ctx context.Context,
	cluster client.Object,
	config map[string]string,
	operationType postgresClient.OperationType_Type,
) (map[string]string, error) {
	tempConfig := config

	contextLogger := log.FromContext(ctx).WithName("enrichConfiguration")

	clusterDefinition, marshalErr := json.Marshal(cluster)
	if marshalErr != nil {
		return nil, marshalErr
	}

	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.PostgresCapabilities(), postgresClient.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION) {
			contextLogger.Debug("skipping plugin", "plugin", plugin.Name())
			continue
		}
		req := &postgresClient.EnrichConfigurationRequest{
			Configs:           config,
			ClusterDefinition: clusterDefinition,
			OperationType:     &postgresClient.OperationType{Type: operationType},
		}
		res, err := plugin.PostgresClient().EnrichConfiguration(ctx, req)
		if err != nil {
			return nil, err
		}
		contextLogger.Debug("received response", "resConfig", res.Configs)
		if len(res.Configs) == 0 {
			continue
		}
		tempConfig = res.Configs
	}

	return tempConfig, nil
}
