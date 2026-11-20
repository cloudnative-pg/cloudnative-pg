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

package promote

import (
	"context"
	"io"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("promote subcommand tests", func() {
	var client k8client.Client
	const namespace = "theNamespace"
	BeforeEach(func() {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		DeferCleanup(func() {
			_ = w.Close()
			os.Stdout = old
			_, _ = io.Copy(GinkgoWriter, r)
		})

		cluster1 := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster1",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster1-1",
				TargetPrimary:  "cluster1-1",
				Phase:          apiv1.PhaseHealthy,
				Conditions: []metav1.Condition{
					{
						Type:    string(apiv1.ConditionClusterReady),
						Status:  metav1.ConditionTrue,
						Reason:  string(apiv1.ClusterReady),
						Message: "Cluster is Ready",
					},
				},
			},
		}
		pods := make([]k8client.Object, 0, 3)
		for _, name := range []string{"cluster1-1", "cluster1-2", "cluster1-3"} {
			pods = append(pods, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			})
		}
		client = fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&cluster1).WithObjects(pods...).WithStatusSubresource(&cluster1).Build()
	})

	It("correctly sets the target primary and the phase if the target pod is present", func(ctx SpecContext) {
		Expect(Promote(ctx, client, namespace, "cluster1", "cluster1-2", false)).
			To(Succeed())
		var cl apiv1.Cluster
		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).
			To(Succeed())
		Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
		Expect(cl.Status.Phase).To(Equal(apiv1.PhaseSwitchover))
		Expect(cl.Status.PhaseReason).To(Equal("Switching over to cluster1-2"))
		Expect(meta.IsStatusConditionTrue(cl.Status.Conditions, string(apiv1.ConditionClusterReady))).
			To(BeFalse())
	})

	It("ignores the promotion if the target pod is missing", func(ctx SpecContext) {
		err := Promote(ctx, client, namespace, "cluster1", "cluster1-missingPod", false)
		Expect(err).To(HaveOccurred())
		var cl apiv1.Cluster
		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).
			To(Succeed())
		Expect(cl.Status.TargetPrimary).To(Equal("cluster1-1"))
		Expect(cl.Status.Phase).To(Equal(apiv1.PhaseHealthy))
		Expect(meta.IsStatusConditionTrue(cl.Status.Conditions, string(apiv1.ConditionClusterReady))).
			To(BeTrue())
	})

	When("a switchover from cluster1-1 to cluster1-2 is in progress", func() {
		getCluster := func(ctx SpecContext) apiv1.Cluster {
			var cl apiv1.Cluster
			ExpectWithOffset(1, client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).
				To(Succeed())
			return cl
		}

		BeforeEach(func(ctx SpecContext) {
			cl := getCluster(ctx)
			cl.Status.TargetPrimary = "cluster1-2"
			cl.Status.Phase = apiv1.PhaseSwitchover
			Expect(client.Status().Update(ctx, &cl)).To(Succeed())
		})

		It("reports that the switchover is in progress when promoting its target", func(ctx SpecContext) {
			Expect(Promote(ctx, client, namespace, "cluster1", "cluster1-2", false)).To(Succeed())

			cl := getCluster(ctx)
			Expect(cl.Status.CurrentPrimary).To(Equal("cluster1-1"))
			Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
		})

		It("does not cancel it without force", func(ctx SpecContext) {
			err := Promote(ctx, client, namespace, "cluster1", "cluster1-1", false)
			Expect(err).To(MatchError(ContainSubstring("--force")))

			cl := getCluster(ctx)
			Expect(cl.Status.CurrentPrimary).To(Equal("cluster1-1"))
			Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
		})

		It("cancels it by promoting the current primary back with force", func(ctx SpecContext) {
			Expect(Promote(ctx, client, namespace, "cluster1", "cluster1-1", true)).To(Succeed())

			cl := getCluster(ctx)
			Expect(cl.Status.CurrentPrimary).To(Equal(cancelSwitchoverMarker))
			Expect(cl.Status.TargetPrimary).To(Equal("cluster1-1"))
			Expect(cl.Status.Phase).To(Equal(apiv1.PhaseSwitchover))
		})

		It("does not promote another instance, even with force", func(ctx SpecContext) {
			err := Promote(ctx, client, namespace, "cluster1", "cluster1-3", true)
			Expect(err).To(MatchError(ContainSubstring("promote cluster1-1 back")))

			cl := getCluster(ctx)
			Expect(cl.Status.CurrentPrimary).To(Equal("cluster1-1"))
			Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
		})
	})

	It("does not name the marker of a cancelled switchover as an instance to promote back", func(ctx SpecContext) {
		var cl apiv1.Cluster
		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		cl.Status.CurrentPrimary = cancelSwitchoverMarker
		Expect(client.Status().Update(ctx, &cl)).To(Succeed())

		err := Promote(ctx, client, namespace, "cluster1", "cluster1-3", true)
		Expect(err).To(MatchError(ContainSubstring("promotion of cluster1-1 is in progress")))
		Expect(err).ToNot(MatchError(ContainSubstring("promote " + cancelSwitchoverMarker)))

		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		Expect(cl.Status.CurrentPrimary).To(Equal(cancelSwitchoverMarker))
		Expect(cl.Status.TargetPrimary).To(Equal("cluster1-1"))
	})

	// changingStatusAfterRead returns a client that changes the status of the
	// cluster after Promote has read it, when it looks for the Pod to promote
	changingStatusAfterRead := func(change func(cl *apiv1.Cluster)) k8client.Client {
		return interceptor.NewClient(client.(k8client.WithWatch), interceptor.Funcs{
			Get: func(
				ctx context.Context, c k8client.WithWatch, key k8client.ObjectKey,
				obj k8client.Object, opts ...k8client.GetOption,
			) error {
				if _, isPod := obj.(*corev1.Pod); isPod {
					var cl apiv1.Cluster
					Expect(c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
					change(&cl)
					Expect(c.Status().Update(ctx, &cl)).To(Succeed())
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})
	}

	It("does not cancel a switchover that completed after the status was read", func(ctx SpecContext) {
		var cl apiv1.Cluster
		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		cl.Status.TargetPrimary = "cluster1-2"
		Expect(client.Status().Update(ctx, &cl)).To(Succeed())

		racingClient := changingStatusAfterRead(func(cl *apiv1.Cluster) {
			cl.Status.CurrentPrimary = "cluster1-2"
		})
		Expect(Promote(ctx, racingClient, namespace, "cluster1", "cluster1-1", true)).
			To(MatchError(ContainSubstring("changed")))

		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		Expect(cl.Status.CurrentPrimary).To(Equal("cluster1-2"))
		Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
	})

	It("does not change the target of a switchover that started after the status was read", func(ctx SpecContext) {
		racingClient := changingStatusAfterRead(func(cl *apiv1.Cluster) {
			cl.Status.TargetPrimary = "cluster1-2"
			cl.Status.Phase = apiv1.PhaseSwitchover
		})
		Expect(Promote(ctx, racingClient, namespace, "cluster1", "cluster1-3", false)).
			To(MatchError(ContainSubstring("changed")))

		var cl apiv1.Cluster
		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		Expect(cl.Status.CurrentPrimary).To(Equal("cluster1-1"))
		Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
		// Nothing is written, not even the condition that follows the phase
		Expect(meta.IsStatusConditionTrue(cl.Status.Conditions, string(apiv1.ConditionClusterReady))).
			To(BeTrue())
	})

	It("lets a person choose the new primary of a failover that is waiting for the quorum", func(ctx SpecContext) {
		var cl apiv1.Cluster
		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		cl.Status.TargetPrimary = apiv1.PendingFailoverMarker
		Expect(client.Status().Update(ctx, &cl)).To(Succeed())

		Expect(Promote(ctx, client, namespace, "cluster1", "cluster1-2", false)).To(Succeed())

		Expect(client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster1"}, &cl)).To(Succeed())
		Expect(cl.Status.CurrentPrimary).To(Equal("cluster1-1"))
		Expect(cl.Status.TargetPrimary).To(Equal("cluster1-2"))
	})
})
