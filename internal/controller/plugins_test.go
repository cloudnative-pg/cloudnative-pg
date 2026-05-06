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
	"errors"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
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
	setClusterStatus    map[string]string
	setClusterStatusErr error
	postReconcileResult pluginClient.ReconcilerHookResult
}

func (f *fakePluginClient) SetStatusInCluster(
	_ context.Context,
	_ k8client.Object,
) (map[string]string, error) {
	return f.setClusterStatus, f.setClusterStatusErr
}

func (f *fakePluginClient) PostReconcile(
	_ context.Context,
	_ k8client.Object,
	_ k8client.Object,
) pluginClient.ReconcilerHookResult {
	return f.postReconcileResult
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

var _ = Describe("finalizeReconciliation", func() {
	const pluginName = "test1_plugin"
	const initialPhase = "InitialPhase"

	var (
		cluster    *apiv1.Cluster
		cli        k8client.Client
		pluginCli  *fakePluginClient
		reconciler *ClusterReconciler
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-suite",
			},
			Status: apiv1.ClusterStatus{
				Phase: initialPhase,
				PluginStatus: []apiv1.PluginStatus{
					{Name: pluginName},
				},
			},
		}
		cli = fake.NewClientBuilder().
			WithObjects(cluster).
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithStatusSubresource(&apiv1.Cluster{}).
			Build()

		pluginCli = &fakePluginClient{}
		reconciler = &ClusterReconciler{
			Client: cli,
		}
	})

	It("registers PhaseHealthy when no plugin sets a status", func(ctx SpecContext) {
		res, err := reconciler.finalizeReconciliation(ctx, pluginCli, cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.IsZero()).To(BeTrue())

		var fresh apiv1.Cluster
		Expect(cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		Expect(fresh.Status.Phase).To(Equal(apiv1.PhaseHealthy))
	})

	It("registers PhaseHealthy and propagates the requeue when plugins patch their statuses", func(ctx SpecContext) {
		// Regression test for the original PR's `!res.IsZero()` short-circuit:
		// setStatusPluginHook returns RequeueAfter=5s on every successful
		// status patch, but that requeue must NOT skip the PhaseHealthy
		// registration. Otherwise clusters with status-reporting plugins
		// (e.g. barman-cloud) would never reach Healthy.
		content, err := json.Marshal(map[string]string{"key": "value"})
		Expect(err).ToNot(HaveOccurred())
		pluginCli.setClusterStatus = map[string]string{pluginName: string(content)}

		res, err := reconciler.finalizeReconciliation(ctx, pluginCli, cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(5 * time.Second))

		var fresh apiv1.Cluster
		Expect(cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		Expect(fresh.Status.Phase).To(Equal(apiv1.PhaseHealthy))
		Expect(fresh.Status.PluginStatus[0].Status).To(BeEquivalentTo(string(content)))
	})

	It("does not register PhaseHealthy when post-reconcile returns an error", func(ctx SpecContext) {
		// Regression test for #8582: when a post-reconcile plugin hook
		// returns an error, the loop must not also mark the cluster
		// Healthy. Otherwise the next reconciliation overwrites the
		// PhaseFailurePlugin set by Reconcile()'s error path and the
		// cluster oscillates between Healthy and FailurePlugin.
		expectedErr := errors.New("plugin post-reconcile failure")
		pluginCli.postReconcileResult = pluginClient.ReconcilerHookResult{Err: expectedErr}
		// Wired up to verify setStatusPluginHook is NOT reached when
		// post-reconcile errored.
		pluginCli.setClusterStatus = map[string]string{pluginName: `"unused"`}

		_, err := reconciler.finalizeReconciliation(ctx, pluginCli, cluster)

		Expect(err).To(MatchError(expectedErr))

		var fresh apiv1.Cluster
		Expect(cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		Expect(fresh.Status.Phase).To(Equal(initialPhase))
		Expect(fresh.Status.PluginStatus[0].Status).To(BeEmpty())
	})

	It("registers PhaseHealthy when post-reconcile requests a requeue without error", func(ctx SpecContext) {
		// Behavior consistency vs pre-#10421 OLD code, which had set
		// PhaseHealthy in reconcileResources before the post-reconcile
		// hook ran. A plugin requesting a requeue (no error) from
		// PostReconcile must still see the cluster transition to Healthy.
		pluginCli.postReconcileResult = pluginClient.ReconcilerHookResult{
			Result: ctrl.Result{RequeueAfter: 30 * time.Second},
		}
		// Wired up to verify setStatusPluginHook is NOT called when
		// post-reconcile already requested a requeue.
		pluginCli.setClusterStatus = map[string]string{pluginName: `"unused"`}

		res, err := reconciler.finalizeReconciliation(ctx, pluginCli, cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(30 * time.Second))

		var fresh apiv1.Cluster
		Expect(cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		Expect(fresh.Status.Phase).To(Equal(apiv1.PhaseHealthy))
		Expect(fresh.Status.PluginStatus[0].Status).To(BeEmpty())
	})

	It("does not register PhaseHealthy when SetStatusInCluster returns an error", func(ctx SpecContext) {
		expectedErr := errors.New("set status failure")
		pluginCli.setClusterStatusErr = expectedErr

		_, err := reconciler.finalizeReconciliation(ctx, pluginCli, cluster)

		// errors.Is to ensure the wrap chain is preserved (% w, not % v):
		// downstream callers depend on Unwrap to classify plugin errors via
		// cnpgiClient.ContainsPluginError.
		Expect(errors.Is(err, expectedErr)).To(BeTrue())

		var fresh apiv1.Cluster
		Expect(cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &fresh)).To(Succeed())
		Expect(fresh.Status.Phase).To(Equal(initialPhase))
	})
})
