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
	"io"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
