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
	"encoding/json"
	"errors"

	"github.com/cloudnative-pg/cnpg-i/pkg/operator"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SetStatusInCluster", func() {
	const pluginName = "fake-plugin"
	const pluginName2 = "fake-plugin2"

	var cluster fakeCluster
	BeforeEach(func() {
		cluster = fakeCluster{}
	})

	It("should correctly set the status of a single plugin", func(ctx SpecContext) {
		pluginStatus := map[string]string{"key": "value"}
		payload, err := json.Marshal(pluginStatus)
		Expect(err).ToNot(HaveOccurred())
		d := data{
			plugins: []connection.Interface{
				newFakeClusterClient(pluginName, payload),
			},
		}
		values, err := d.SetStatusInCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(values[pluginName]).To(BeEquivalentTo(string(payload)))
	})

	It("should report an empty map given that the plugin doesn't send a json back", func(ctx SpecContext) {
		d := data{
			plugins: []connection.Interface{newFakeClusterClient(pluginName, nil)},
		}
		values, err := d.SetStatusInCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(values).To(BeEquivalentTo(map[string]string{}))
	})

	It("should be able to set multiple plugins statuses", func(ctx SpecContext) {
		pluginStatus1 := map[string]string{"key": "value"}
		payload1, err := json.Marshal(pluginStatus1)
		Expect(err).ToNot(HaveOccurred())

		pluginStatus2 := map[string]string{"key1": "value1"}
		payload2, err := json.Marshal(pluginStatus2)
		Expect(err).ToNot(HaveOccurred())

		d := data{
			plugins: []connection.Interface{
				newFakeClusterClient(pluginName, payload1),
				newFakeClusterClient(pluginName2, payload2),
			},
		}
		values, err := d.SetStatusInCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(values).To(BeEquivalentTo(map[string]string{
			pluginName:  string(payload1),
			pluginName2: string(payload2),
		}))
	})

	It("should report an error in case of an invalid json", func(ctx SpecContext) {
		d := data{
			plugins: []connection.Interface{
				newFakeClusterClient(pluginName, []byte("random")),
			},
		}
		_, err := d.SetStatusInCluster(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(errInvalidJSON))
	})

	It("should report an error in case of an error returned by the underlying SetStatusInCluster", func(ctx SpecContext) {
		cli := newFakeClusterClient(pluginName, []byte("random"))
		expectedErr := errors.New("bad request")
		cli.operatorClient.errSetStatusInCluster = expectedErr
		d := data{
			plugins: []connection.Interface{cli},
		}
		_, err := d.SetStatusInCluster(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(errSetStatusInCluster))
		Expect(err).To(MatchError(expectedErr))
	})
})

func newFakeClusterClient(name string, jsonStatus []byte) *fakeConnection {
	fc := &fakeConnection{
		name: name,
		operatorClient: &fakeOperatorClient{
			capabilities: &operator.OperatorCapabilitiesResult{
				Capabilities: []*operator.OperatorCapability{
					{
						Type: &operator.OperatorCapability_Rpc{
							Rpc: &operator.OperatorCapability_RPC{
								Type: operator.OperatorCapability_RPC_TYPE_SET_STATUS_IN_CLUSTER,
							},
						},
					},
				},
			},
		},
	}
	fc.setStatusResponse(jsonStatus)
	return fc
}
