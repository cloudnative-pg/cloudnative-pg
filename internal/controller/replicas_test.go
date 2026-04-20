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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sacrificial Pod detection", func() {
	car1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "car-1",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "1",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	car2 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "car-2",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "2",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	foo := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "3",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	bar := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bar",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "4",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	It("detects if the list of Pods is empty", func() {
		var podList []corev1.Pod
		Expect(findDeletableInstance(&apiv1.Cluster{}, podList)).To(BeEmpty())
	})

	It("detects if we have not a ready Pod", func() {
		podList := []corev1.Pod{foo, bar}
		Expect(findDeletableInstance(&apiv1.Cluster{}, podList)).To(BeEmpty())
	})

	It("detects it if is the first available", func() {
		podList := []corev1.Pod{foo, bar, car1, car2}
		resultName := findDeletableInstance(&apiv1.Cluster{}, podList)
		Expect(resultName).ToNot(BeEmpty())
		Expect(resultName).To(Equal("car-2"))
	})

	It("detects it if is not the first one", func() {
		podList := []corev1.Pod{car2, foo, bar, car1}
		resultName := findDeletableInstance(&apiv1.Cluster{}, podList)
		Expect(resultName).ToNot(BeEmpty())
		Expect(resultName).To(Equal("car-2"))
	})
})

var _ = Describe("markOldPrimaryAsUnhealthy", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	makePod := func(name, namespace, role string) corev1.Pod {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{},
			},
		}
		if role != "" {
			utils.SetInstanceRole(&pod.ObjectMeta, role)
		}
		return pod
	}

	It("changes the primary label from the old primary pod", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		primary := makePod("cluster-1", namespace, specs.ClusterRoleLabelPrimary)
		replica1 := makePod("cluster-2", namespace, specs.ClusterRoleLabelReplica)
		replica2 := makePod("cluster-3", namespace, specs.ClusterRoleLabelReplica)

		for i, pod := range []corev1.Pod{primary, replica1, replica2} {
			p := pod
			Expect(env.client.Create(ctx, &p)).To(Succeed())
			// refresh the local copy with server-assigned fields
			if i == 0 {
				primary = p
			}
		}

		pods := []corev1.Pod{primary, replica1, replica2}

		err := env.clusterReconciler.markOldPrimaryAsUnhealthy(ctx, "cluster-1", pods)
		Expect(err).ToNot(HaveOccurred())

		// Verify the old primary's label was changed to unhealthy on the API server
		var updated corev1.Pod
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&primary), &updated)).To(Succeed())
		Expect(updated.Labels[utils.ClusterInstanceRoleLabelName]).To(Equal(specs.ClusterRoleLabelUnhealthy))
		//nolint:staticcheck
		Expect(updated.Labels[utils.ClusterRoleLabelName]).To(Equal(specs.ClusterRoleLabelUnhealthy))

		// Verify replica pods are unchanged
		var replica1Updated corev1.Pod
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&replica1), &replica1Updated)).To(Succeed())
		Expect(replica1Updated.Labels[utils.ClusterInstanceRoleLabelName]).To(Equal(specs.ClusterRoleLabelReplica))
	})

	It("does not error when the old primary is not in the pod list", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		replica := makePod("cluster-2", namespace, specs.ClusterRoleLabelReplica)
		Expect(env.client.Create(ctx, &replica)).To(Succeed())

		err := env.clusterReconciler.markOldPrimaryAsUnhealthy(ctx, "cluster-1", []corev1.Pod{replica})
		Expect(err).ToNot(HaveOccurred())
	})

	It("is a no-op when the old primary already has the unhealthy label", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		pod := makePod("cluster-1", namespace, specs.ClusterRoleLabelUnhealthy)
		Expect(env.client.Create(ctx, &pod)).To(Succeed())

		err := env.clusterReconciler.markOldPrimaryAsUnhealthy(ctx, "cluster-1", []corev1.Pod{pod})
		Expect(err).ToNot(HaveOccurred())

		var updated corev1.Pod
		Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&pod), &updated)).To(Succeed())
		Expect(updated.Labels[utils.ClusterInstanceRoleLabelName]).To(Equal(specs.ClusterRoleLabelUnhealthy))
	})

	It("surfaces the Patch error so callers can apply their best-effort or retry strategy", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)

		primary := makePod("cluster-1", namespace, specs.ClusterRoleLabelPrimary)

		failingClient := fake.NewClientBuilder().
			WithScheme(env.scheme).
			WithObjects(&primary).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, obj client.Object,
					_ client.Patch, _ ...client.PatchOption,
				) error {
					Expect(obj).To(BeAssignableToTypeOf(&corev1.Pod{}))
					Expect(obj.GetName()).To(Equal("cluster-1"))
					Expect(obj.GetNamespace()).To(Equal(namespace))
					return fmt.Errorf("simulated API server error")
				},
			}).
			Build()

		r := &ClusterReconciler{Client: failingClient, Scheme: env.scheme}

		err := r.markOldPrimaryAsUnhealthy(ctx, "cluster-1", []corev1.Pod{primary})
		Expect(err).To(MatchError(ContainSubstring("simulated API server error")))
	})
})

var _ = Describe("Check pods not on primary node", func() {
	item1 := postgres.PostgresqlStatus{
		IsPrimary: false,
		Node:      "node-1",
		Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
	}

	item2 := postgres.PostgresqlStatus{
		IsPrimary: false,
		Node:      "node-2",
		Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
	}
	statusList := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{item1, item2}}

	It("if primary is nil", func() {
		Expect(GetPodsNotOnPrimaryNode(statusList, nil).Items).To(BeEmpty())
	})

	item1.IsPrimary = true
	statusList2 := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{item1, item2}}

	It("first status element is primary", func() {
		Expect(GetPodsNotOnPrimaryNode(statusList2, &statusList2.Items[0]).Items).ToNot(BeEmpty())
	})
})
