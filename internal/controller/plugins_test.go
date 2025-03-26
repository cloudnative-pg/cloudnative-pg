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

package controller

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakePluginClient struct {
	pluginClient.Client
	setClusterStatus map[string]string
}

func (f *fakePluginClient) SetStatusInCluster(
	_ context.Context,
	_ k8client.Object,
) (map[string]string, error) {
	return f.setClusterStatus, nil
}

var _ = Describe("setStatusPluginHook", func() {
	const pluginName = "test1_plugin"
	var (
		cluster   *apiv1.Cluster
		cli       k8client.Client
		pluginCli *fakePluginClient
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-suite",
			},
			Status: apiv1.ClusterStatus{
				PluginStatus: []apiv1.PluginStatus{
					{
						Name: pluginName,
					},
				},
			},
		}
		cli = fake.NewClientBuilder().
			WithObjects(cluster).
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithStatusSubresource(&apiv1.Cluster{}).
			Build()

		pluginCli = &fakePluginClient{}
	})

	It("should properly populated the plugin status", func(ctx SpecContext) {
		content, err := json.Marshal(map[string]string{"key": "value"})
		Expect(err).ToNot(HaveOccurred())
		pluginCli.setClusterStatus = map[string]string{pluginName: string(content)}
		res, err := setStatusPluginHook(ctx, cli, pluginCli, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(cluster.Status.PluginStatus[0].Status).To(BeEquivalentTo(string(content)))
	})
})
