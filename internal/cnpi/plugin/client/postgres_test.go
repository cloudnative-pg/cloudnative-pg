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

package client

import (
	"context"
	"errors"

	"github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
		cluster *apiv1.Cluster
		config  map[string]string
	)

	BeforeEach(func() {
		d = &data{plugins: []connection.Interface{}}

		cluster = &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-namespace"}}
		config = map[string]string{"key1": "value1"}
	})

	It("should successfully enrich configuration", func(ctx SpecContext) {
		postgresClient := &fakePostgresClient{
			enrichConfigResponse: &postgres.EnrichConfigurationResult{
				Configs: map[string]string{"key2": "value2"},
			},
		}

		plugin := &fakePostgresConnection{
			name:           "test-plugin",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient,
		}

		d.plugins = append(d.plugins, plugin)

		result, err := d.EnrichConfiguration(ctx, cluster, config)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveKeyWithValue("key1", "value1"))
		Expect(result).To(HaveKeyWithValue("key2", "value2"))
	})

	It("should return error when plugin returns error", func(ctx SpecContext) {
		expectedErr := errors.New("plugin error")

		postgresClient := &fakePostgresClient{
			enrichConfigError: expectedErr,
		}

		plugin := &fakePostgresConnection{
			name:           "test-plugin",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient,
		}

		d.plugins = append(d.plugins, plugin)

		_, err := d.EnrichConfiguration(ctx, cluster, config)

		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(expectedErr))
	})

	It("should skip plugins without required capability", func(ctx SpecContext) {
		plugin := &fakePostgresConnection{
			name:         "test-plugin",
			capabilities: []postgres.PostgresCapability_RPC_Type{},
		}

		d.plugins = append(d.plugins, plugin)

		result, err := d.EnrichConfiguration(ctx, cluster, config)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(config))
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
				Configs: map[string]string{"key3": "value3"},
			},
		}

		plugin2 := &fakePostgresConnection{
			name:           "plugin2",
			capabilities:   []postgres.PostgresCapability_RPC_Type{postgres.PostgresCapability_RPC_TYPE_ENRICH_CONFIGURATION},
			postgresClient: postgresClient2,
		}

		d.plugins = append(d.plugins, plugin1, plugin2)

		result, err := d.EnrichConfiguration(ctx, cluster, config)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveKeyWithValue("key1", "value1"))
		Expect(result).To(HaveKeyWithValue("key2", "value2"))
		Expect(result).To(HaveKeyWithValue("key3", "value3"))
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

		result, err := d.EnrichConfiguration(ctx, cluster, config)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveKeyWithValue("key1", "overwritten-value"))
		Expect(result).To(HaveLen(1))
	})
})
