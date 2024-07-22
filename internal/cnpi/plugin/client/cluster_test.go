package client

import (
	"encoding/json"
	"errors"

	"github.com/cloudnative-pg/cnpg-i/pkg/operator"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SetClusterStatus", func() {
	const pluginName = "fake-plugin"
	const pluginName2 = "fake-plugin2"

	var cluster *apiv1.Cluster
	BeforeEach(func() {
		cluster = &apiv1.Cluster{}
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
		values, err := d.SetClusterStatus(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(values[pluginName]).To(BeEquivalentTo(string(payload)))
	})

	It("should report an empty map given that the plugin doesn't send a json back", func(ctx SpecContext) {
		d := data{
			plugins: []connection.Interface{newFakeClusterClient(pluginName, nil)},
		}
		values, err := d.SetClusterStatus(ctx, cluster)
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
		values, err := d.SetClusterStatus(ctx, cluster)
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
		_, err := d.SetClusterStatus(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(errInvalidJSON))
	})

	It("should report an error in case of an error returned by the underlying SetClusterStatus", func(ctx SpecContext) {
		cli := newFakeClusterClient(pluginName, []byte("random"))
		expectedErr := errors.New("bad request")
		cli.operatorClient.errSetClusterStatus = expectedErr
		d := data{
			plugins: []connection.Interface{cli},
		}
		_, err := d.SetClusterStatus(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(errSetClusterStatus))
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
								Type: operator.OperatorCapability_RPC_TYPE_SET_CLUSTER_STATUS,
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
