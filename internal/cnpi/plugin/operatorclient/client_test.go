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

package operatorclient

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	pluginclient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeClusterCRD struct {
	k8client.Object
	pluginClient *fakePluginClient
}

func (f *fakeClusterCRD) LoadPluginClient(_ context.Context) (pluginclient.Client, error) {
	return f.pluginClient, nil
}

type fakePluginClient struct {
	pluginclient.Client
	injectLabels map[string]string
}

func (f fakePluginClient) LifecycleHook(
	_ context.Context,
	_ plugin.OperationVerb,
	_ k8client.Object,
	object k8client.Object,
) (k8client.Object, error) {
	object.SetLabels(f.injectLabels)
	return object, nil
}

var _ = Describe("extendedClient", func() {
	var (
		c              *extendedClient
		expectedLabels map[string]string
		pluginClient   *fakePluginClient
	)

	BeforeEach(func() {
		c = &extendedClient{
			Client: fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).Build(),
		}
		expectedLabels = map[string]string{"lifecycle": "true"}
		pluginClient = &fakePluginClient{
			injectLabels: expectedLabels,
		}
	})

	It("invokePlugin", func(ctx SpecContext) {
		fakeCrd := &fakeClusterCRD{
			pluginClient: pluginClient,
		}
		newCtx := context.WithValue(ctx, utils.ContextKeyCluster, fakeCrd)
		By("ensuring it works the first invocation", func() {
			obj, err := c.invokePlugin(newCtx, plugin.OperationVerbCreate, &corev1.Pod{})
			Expect(err).ToNot(HaveOccurred())
			Expect(obj.GetLabels()).To(Equal(expectedLabels))
		})

		By("ensuring it maintains the reference for subsequent invocations", func() {
			newLabels := map[string]string{"test": "test"}
			pluginClient.injectLabels = newLabels
			obj, err := c.invokePlugin(newCtx, plugin.OperationVerbCreate, &corev1.Pod{})
			Expect(err).ToNot(HaveOccurred())
			Expect(obj.GetLabels()).To(Equal(newLabels))
		})
	})
})
