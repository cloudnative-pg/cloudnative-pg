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

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiclient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("updatePluginsStatus", func() {
	const (
		pluginName      = "test1_plugin"
		otherPluginName = "test2_plugin"
		pluginStatus    = `{"status": "testing"}`
	)

	var (
		ctx           context.Context
		cluster       *apiv1.Cluster
		fakeClient    client.Client
		pluginCli     *fakePluginClient
		reconciler    *ClusterReconciler
		statusPatches int
	)

	// metadataFor builds the metadata a loaded plugin reports.
	metadataFor := func(names ...string) []connection.Metadata {
		result := make([]connection.Metadata, len(names))
		for i, name := range names {
			result[i] = connection.Metadata{
				Name:                       name,
				Version:                    "0.0.1",
				Capabilities:               []string{"TYPE_OPERATOR_SERVICE"},
				OperatorCapabilities:       []string{"TYPE_SET_STATUS_IN_CLUSTER"},
				WALCapabilities:            []string{},
				BackupCapabilities:         []string{},
				RestoreJobHookCapabilities: []string{},
			}
		}
		return result
	}

	BeforeEach(func() {
		ctx = context.Background()
		statusPatches = 0
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
			Status: apiv1.ClusterStatus{
				PluginStatus: []apiv1.PluginStatus{
					{
						Name:                 pluginName,
						Version:              "0.0.1",
						Capabilities:         []string{"TYPE_OPERATOR_SERVICE"},
						OperatorCapabilities: []string{"TYPE_SET_STATUS_IN_CLUSTER"},
						Status:               pluginStatus,
					},
				},
			},
		}

		fakeClient = fake.NewClientBuilder().
			WithObjects(cluster).
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					ctx context.Context,
					c client.Client,
					subResourceName string,
					obj client.Object,
					patch client.Patch,
					opts ...client.SubResourcePatchOption,
				) error {
					if subResourceName == "status" {
						statusPatches++
					}
					return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		pluginCli = &fakePluginClient{}
		reconciler = &ClusterReconciler{Client: fakeClient}
	})

	It("preserves the status reported by the plugin across the rebuild", func() {
		pluginCli.metadataList = metadataFor(pluginName)
		ctx = cnpgiclient.SetPluginClientInContext(ctx, pluginCli)

		Expect(reconciler.updatePluginsStatus(ctx, cluster)).To(Succeed())

		Expect(cluster.Status.PluginStatus).To(HaveLen(1))
		Expect(cluster.Status.PluginStatus[0].Name).To(Equal(pluginName))
		Expect(cluster.Status.PluginStatus[0].Status).To(Equal(pluginStatus))

		Expect(statusPatches).To(BeZero())
	})

	It("patches the status when the plugin metadata changed", func() {
		pluginCli.metadataList = metadataFor(pluginName)
		pluginCli.metadataList[0].Version = "0.0.2"
		ctx = cnpgiclient.SetPluginClientInContext(ctx, pluginCli)

		Expect(reconciler.updatePluginsStatus(ctx, cluster)).To(Succeed())

		Expect(statusPatches).To(Equal(1))
		Expect(cluster.Status.PluginStatus[0].Version).To(Equal("0.0.2"))
		// A metadata change must not cost us the plugin-reported status.
		Expect(cluster.Status.PluginStatus[0].Status).To(Equal(pluginStatus))
	})

	It("keeps each plugin's status with its own entry when a plugin is added", func() {
		// The carry-over is by name, not by index, so a plugin appearing
		// before the existing one in the sorted list must not shift statuses
		// onto the wrong entry.
		pluginCli.metadataList = metadataFor(otherPluginName, pluginName)
		ctx = cnpgiclient.SetPluginClientInContext(ctx, pluginCli)

		Expect(reconciler.updatePluginsStatus(ctx, cluster)).To(Succeed())

		Expect(cluster.Status.PluginStatus).To(HaveLen(2))
		Expect(cluster.Status.PluginStatus[0].Name).To(Equal(otherPluginName))
		Expect(cluster.Status.PluginStatus[0].Status).To(BeEmpty())
		Expect(cluster.Status.PluginStatus[1].Name).To(Equal(pluginName))
		Expect(cluster.Status.PluginStatus[1].Status).To(Equal(pluginStatus))
	})

	It("drops the entry of a plugin that is no longer loaded", func() {
		pluginCli.metadataList = metadataFor(otherPluginName)
		ctx = cnpgiclient.SetPluginClientInContext(ctx, pluginCli)

		Expect(reconciler.updatePluginsStatus(ctx, cluster)).To(Succeed())

		Expect(cluster.Status.PluginStatus).To(HaveLen(1))
		Expect(cluster.Status.PluginStatus[0].Name).To(Equal(otherPluginName))
	})
})

var _ = Describe("mapPluginEndpointSlicesToClusters", func() {
	const (
		operatorNamespace = "cnpg-system"
		pluginName        = "plugin-a"
		pluginServiceName = "plugin-a-svc"
	)

	var (
		ctx        context.Context
		reconciler *ClusterReconciler
	)

	pluginService := func() *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pluginServiceName,
				Namespace: operatorNamespace,
				Labels: map[string]string{
					utils.PluginNameLabelName: pluginName,
				},
				Annotations: map[string]string{
					utils.PluginClientSecretAnnotationName: "client-secret",
					utils.PluginServerSecretAnnotationName: "server-secret",
					utils.PluginPortAnnotationName:         "9090",
				},
			},
		}
	}

	endpointSlice := func() *discoveryv1.EndpointSlice {
		return &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pluginServiceName + "-abcde",
				Namespace: operatorNamespace,
				Labels: map[string]string{
					discoveryv1.LabelServiceName: pluginServiceName,
				},
			},
		}
	}

	clusterUsingPlugin := func(namespace, name string) *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{{Name: pluginName}},
			},
		}
	}

	buildReconciler := func(objects ...client.Object) *ClusterReconciler {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithIndex(&apiv1.Cluster{}, usedPluginsClusterKey, func(rawObj client.Object) []string {
				return getPluginsNeededForReconcile(rawObj.(*apiv1.Cluster))
			}).
			WithObjects(objects...).
			Build()
		return &ClusterReconciler{Client: fakeClient}
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("enqueues every cluster using the plugin when its EndpointSlice changes", func() {
		reconciler = buildReconciler(
			pluginService(),
			clusterUsingPlugin("app-ns-1", "cluster-1"),
			clusterUsingPlugin("app-ns-2", "cluster-2"),
		)
		mapFn := reconciler.mapPluginEndpointSlicesToClusters(operatorNamespace)

		requests := mapFn(ctx, endpointSlice())
		Expect(requests).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "app-ns-1", Name: "cluster-1"}},
			reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "app-ns-2", Name: "cluster-2"}},
		))
	})

	It("enqueues only the clusters using the plugin whose EndpointSlice changed", func() {
		// A cluster wired to a different plugin must not be enqueued. This is
		// what the usedPlugins field-index filter guarantees: without it, every
		// cluster would be reconciled on any plugin rollout. The ConsistOf below
		// is exact, so it fails if the unrelated cluster leaks into the result.
		clusterUsingOtherPlugin := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "other-cluster", Namespace: "app-ns-other"},
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{{Name: "some-other-plugin"}},
			},
		}
		reconciler = buildReconciler(
			pluginService(),
			clusterUsingPlugin("app-ns-1", "cluster-1"),
			clusterUsingOtherPlugin,
		)
		mapFn := reconciler.mapPluginEndpointSlicesToClusters(operatorNamespace)

		requests := mapFn(ctx, endpointSlice())
		Expect(requests).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "app-ns-1", Name: "cluster-1"}},
		))
	})

	It("returns nil when the owning Service does not look like a plugin", func() {
		// Service exists but is missing the plugin label.
		nonPlugin := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pluginServiceName,
				Namespace: operatorNamespace,
			},
		}
		reconciler = buildReconciler(nonPlugin, clusterUsingPlugin("app-ns", "cluster"))
		mapFn := reconciler.mapPluginEndpointSlicesToClusters(operatorNamespace)

		Expect(mapFn(ctx, endpointSlice())).To(BeNil())
	})

	It("returns nil when the owning Service has been deleted", func() {
		reconciler = buildReconciler(clusterUsingPlugin("app-ns", "cluster"))
		mapFn := reconciler.mapPluginEndpointSlicesToClusters(operatorNamespace)

		Expect(mapFn(ctx, endpointSlice())).To(BeNil())
	})

	It("returns nil when the EndpointSlice has no service-name label", func() {
		reconciler = buildReconciler(pluginService(), clusterUsingPlugin("app-ns", "cluster"))
		mapFn := reconciler.mapPluginEndpointSlicesToClusters(operatorNamespace)

		slice := endpointSlice()
		slice.Labels = nil
		Expect(mapFn(ctx, slice)).To(BeNil())
	})

	It("returns no requests when no cluster references the plugin", func() {
		reconciler = buildReconciler(pluginService())
		mapFn := reconciler.mapPluginEndpointSlicesToClusters(operatorNamespace)

		Expect(mapFn(ctx, endpointSlice())).To(BeEmpty())
	})
})
