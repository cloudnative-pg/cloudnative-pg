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

	"github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"google.golang.org/grpc"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakePostgresClient struct {
	enrichConfigResponse *postgres.EnrichConfigurationResult
	enrichConfigError    error
}

type fakePostgresConnection struct {
	name           string
	capabilities   []postgres.PostgresCapability_RPC_Type
	postgresClient *fakePostgresClient
	connection.Interface
}

func (f *fakePostgresClient) GetCapabilities(
	_ context.Context,
	_ *postgres.PostgresCapabilitiesRequest,
	_ ...grpc.CallOption,
) (*postgres.PostgresCapabilitiesResult, error) {
	return &postgres.PostgresCapabilitiesResult{
		Capabilities: []*postgres.PostgresCapability{
			{
				Type: &postgres.PostgresCapability_Rpc{
					Rpc: &postgres.PostgresCapability_RPC{
						Type: postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION,
					},
				},
			},
		},
	}, nil
}

func (f *fakePostgresClient) EnrichConfiguration(
	_ context.Context,
	_ *postgres.EnrichConfigurationRequest,
	_ ...grpc.CallOption,
) (*postgres.EnrichConfigurationResult, error) {
	return f.enrichConfigResponse, f.enrichConfigError
}

func (f *fakePostgresConnection) Name() string {
	return f.name
}

func (f *fakePostgresConnection) PostgresClient() postgres.PostgresClient {
	return f.postgresClient
}

func (f *fakePostgresConnection) PostgresCapabilities() []postgres.PostgresCapability_RPC_Type {
	return f.capabilities
}

var _ = Describe("EnrichConfiguration", func() {
	var (
		d       *data
		cluster *fakeCluster
		config  map[string]string
	)

	BeforeEach(func() {
		config = map[string]string{"key1": "value1"}
		d = &data{plugins: []connection.Interface{}}

		cluster = &fakeCluster{}
	})

	It("should successfully enrich configuration", func(ctx SpecContext) {
		postgresClient := &fakePostgresClient{
			enrichConfigResponse: &postgres.EnrichConfigurationResult{
				Configs: map[string]string{"key1": "value1", "key2": "value2"},
			},
		}

		plugin := &fakePostgresConnection{
			name:           "test-plugin",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient,
		}

		d.plugins = append(d.plugins, plugin)

		config, err := d.EnrichConfiguration(ctx, cluster, config, postgres.OperationType_TYPE_UNSPECIFIED)

		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(HaveKeyWithValue("key1", "value1"))
		Expect(config).To(HaveKeyWithValue("key2", "value2"))
	})

	It("should return error when plugin returns error", func(ctx SpecContext) {
		expectedErr := newPluginError("plugin error")

		postgresClient := &fakePostgresClient{
			enrichConfigError: expectedErr,
		}

		plugin := &fakePostgresConnection{
			name:           "test-plugin",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient,
		}

		d.plugins = append(d.plugins, plugin)

		_, err := d.EnrichConfiguration(ctx, cluster, config, postgres.OperationType_TYPE_UNSPECIFIED)

		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(expectedErr))
	})

	It("should skip plugins without required capability", func(ctx SpecContext) {
		plugin := &fakePostgresConnection{
			name:         "test-plugin",
			capabilities: []postgres.PostgresCapability_RPC_Type{},
		}

		d.plugins = append(d.plugins, plugin)

		origMap := cloneMap(config)

		config, err := d.EnrichConfiguration(ctx, cluster, config, postgres.OperationType_TYPE_UNSPECIFIED)

		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(BeEquivalentTo(origMap))
	})

	It("should merge configurations from multiple plugins", func(ctx SpecContext) {
		postgresClient1 := &fakePostgresClient{
			enrichConfigResponse: &postgres.EnrichConfigurationResult{
				Configs: map[string]string{"key2": "value2"},
			},
		}

		plugin1 := &fakePostgresConnection{
			name:           "plugin1",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient1,
		}

		postgresClient2 := &fakePostgresClient{
			enrichConfigResponse: &postgres.EnrichConfigurationResult{
				Configs: map[string]string{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				},
			},
		}

		plugin2 := &fakePostgresConnection{
			name:           "plugin2",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient2,
		}

		d.plugins = append(d.plugins, plugin1, plugin2)

		config, err := d.EnrichConfiguration(ctx, cluster, config, postgres.OperationType_TYPE_UNSPECIFIED)

		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(HaveKeyWithValue("key1", "value1"))
		Expect(config).To(HaveKeyWithValue("key2", "value2"))
		Expect(config).To(HaveKeyWithValue("key3", "value3"))
	})

	It("should overwrite existing config key when plugin returns the same key", func(ctx SpecContext) {
		postgresClient := &fakePostgresClient{
			enrichConfigResponse: &postgres.EnrichConfigurationResult{
				Configs: map[string]string{"key1": "overwritten-value"},
			},
		}

		plugin := &fakePostgresConnection{
			name:           "test-plugin",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient,
		}

		d.plugins = append(d.plugins, plugin)

		config, err := d.EnrichConfiguration(ctx, cluster, config, postgres.OperationType_TYPE_UNSPECIFIED)

		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(HaveKeyWithValue("key1", "overwritten-value"))
		Expect(config).To(HaveLen(1))
	})
})

func cloneMap(original map[string]string) map[string]string {
	clone := make(map[string]string, len(original))
	for k, v := range original {
		clone[k] = v
	}
	return clone
}
