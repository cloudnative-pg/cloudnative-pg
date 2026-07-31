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
					Type:   corev1.PodReady,
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
					Type:   corev1.PodReady,
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
					Type:   corev1.PodReady,
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
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	// unreachable mimics a Pod whose node has stopped reporting to the API
	// server: the node lifecycle controller flips PodReady to False, but the
	// stale ContainersReady left by the kubelet remains True.
	unreachable := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unreachable",
			Namespace: "default",
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: "5",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
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

	It("skips a Pod whose node is unreachable even if it has the highest serial", func() {
		podList := []corev1.Pod{car1, car2, unreachable}
		resultName := findDeletableInstance(&apiv1.Cluster{}, podList)
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

var _ = Describe("Check schedulable pods not on primary node", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

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
		ctx := context.Background()
		Expect(env.clusterReconciler.getSchedulablePodsNotOnPrimaryNode(ctx, statusList, nil).Items).To(BeEmpty())
	})

	item1.IsPrimary = true
	statusList2 := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{item1, item2}}

	It("first status element is primary", func() {
		ctx := context.Background()

		node2 := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
		}
		Expect(env.client.Create(ctx, &node2)).To(Succeed())

		Expect(env.clusterReconciler.getSchedulablePodsNotOnPrimaryNode(ctx, statusList2, &statusList2.Items[0]).Items).
			ToNot(BeEmpty())
	})

	It("excludes pods on unschedulable nodes", func() {
		ctx := context.Background()

		primaryNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "primary-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		cordonedNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cordoned-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		schedulableNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-node"},
		}
		Expect(env.client.Create(ctx, &primaryNode)).To(Succeed())
		Expect(env.client.Create(ctx, &cordonedNode)).To(Succeed())
		Expect(env.client.Create(ctx, &schedulableNode)).To(Succeed())

		primary := postgres.PostgresqlStatus{
			IsPrimary: true,
			Node:      "primary-node",
			Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-primary"}},
		}
		onCordonedNode := postgres.PostgresqlStatus{
			IsPrimary: false,
			Node:      "cordoned-node",
			Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-cordoned"}},
		}
		onMissingNode := postgres.PostgresqlStatus{
			IsPrimary: false,
			Node:      "missing-node",
			Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-missing-node"}},
		}
		onHealthyNode := postgres.PostgresqlStatus{
			IsPrimary: false,
			Node:      "healthy-node",
			Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-healthy"}},
		}
		list := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{primary, onCordonedNode, onMissingNode, onHealthyNode},
		}

		result := env.clusterReconciler.getSchedulablePodsNotOnPrimaryNode(ctx, list, &primary)
		Expect(result.Items).To(HaveLen(1))
		Expect(result.Items[0].Pod.Name).To(Equal("pod-healthy"))
	})
})

var _ = Describe("shouldSetPrimaryToSchedulableNode", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	newStatus := func(podName, node string, isPrimary bool) postgres.PostgresqlStatus {
		return postgres.PostgresqlStatus{
			IsPrimary: isPrimary,
			Node:      node,
			Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName}},
		}
	}

	It("returns false when the primary is not on an unschedulable node", func() {
		ctx := context.Background()
		schedulableNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "primary-node"}}
		Expect(env.client.Create(ctx, &schedulableNode)).To(Succeed())

		primary := newStatus("pod-primary", "primary-node", true)
		cluster := &apiv1.Cluster{
			Spec:   apiv1.ClusterSpec{Instances: 3},
			Status: apiv1.ClusterStatus{ReadyInstances: 3},
		}
		list := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{primary}}

		Expect(env.clusterReconciler.shouldSetPrimaryToSchedulableNode(ctx, cluster, list, &primary)).To(BeFalse())
	})

	It("returns false when the primary's node can't be found", func() {
		ctx := context.Background()

		primary := newStatus("pod-primary", "missing-node", true)
		cluster := &apiv1.Cluster{
			Spec:   apiv1.ClusterSpec{Instances: 3},
			Status: apiv1.ClusterStatus{ReadyInstances: 3},
		}
		list := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{primary}}

		Expect(env.clusterReconciler.shouldSetPrimaryToSchedulableNode(ctx, cluster, list, &primary)).To(BeFalse())
	})

	It("returns false when the primary is on an unschedulable node but it is the only instance in the cluster", func() {
		ctx := context.Background()

		primaryNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "primary-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		Expect(env.client.Create(ctx, &primaryNode)).To(Succeed())

		primary := newStatus("pod-primary", "primary-node", true)
		cluster := &apiv1.Cluster{
			Spec:   apiv1.ClusterSpec{Instances: 1},
			Status: apiv1.ClusterStatus{ReadyInstances: 1},
		}
		list := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{primary}}

		Expect(env.clusterReconciler.shouldSetPrimaryToSchedulableNode(ctx, cluster, list, &primary)).To(BeFalse())
	})

	It("returns false when the primary is on an unschedulable node but instances are still becoming ready", func() {
		ctx := context.Background()
		primaryNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "primary-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		healthyNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "healthy-node"}}
		Expect(env.client.Create(ctx, &primaryNode)).To(Succeed())
		Expect(env.client.Create(ctx, &healthyNode)).To(Succeed())

		primary := newStatus("pod-primary", "primary-node", true)
		replica := newStatus("pod-replica", "healthy-node", false)
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{Instances: 3},
			// one instance is still joining, so we shouldn't disrupt the primary yet
			Status: apiv1.ClusterStatus{ReadyInstances: 2},
		}
		list := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{primary, replica}}

		Expect(env.clusterReconciler.shouldSetPrimaryToSchedulableNode(ctx, cluster, list, &primary)).To(BeFalse())
	})

	It("returns false when not all the replicas have moved to a schedulable node yet", func() {
		ctx := context.Background()
		primaryNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "primary-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		unhealthyNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "unhealthy-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		healthyNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "healthy-node"}}
		Expect(env.client.Create(ctx, &primaryNode)).To(Succeed())
		Expect(env.client.Create(ctx, &unhealthyNode)).To(Succeed())
		Expect(env.client.Create(ctx, &healthyNode)).To(Succeed())

		primary := newStatus("pod-primary", "primary-node", true)
		// still stuck on the primary's (unschedulable) node
		replicaNotYetMoved := newStatus("pod-replica-1", "unhealthy-node", false)
		replicaMoved := newStatus("pod-replica-2", "healthy-node", false)
		list := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{primary, replicaNotYetMoved, replicaMoved},
		}
		cluster := &apiv1.Cluster{
			Spec:   apiv1.ClusterSpec{Instances: 3},
			Status: apiv1.ClusterStatus{ReadyInstances: 3},
		}

		Expect(env.clusterReconciler.shouldSetPrimaryToSchedulableNode(ctx, cluster, list, &primary)).To(BeFalse())
	})

	It("returns true when the primary is unschedulable and all replicas have moved to schedulable nodes", func() {
		ctx := context.Background()
		primaryNode := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "primary-node"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
		}
		healthyNode1 := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "healthy-node-1"}}
		healthyNode2 := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "healthy-node-2"}}
		Expect(env.client.Create(ctx, &primaryNode)).To(Succeed())
		Expect(env.client.Create(ctx, &healthyNode1)).To(Succeed())
		Expect(env.client.Create(ctx, &healthyNode2)).To(Succeed())

		primary := newStatus("pod-primary", "primary-node", true)
		replica1 := newStatus("pod-replica-1", "healthy-node-1", false)
		replica2 := newStatus("pod-replica-2", "healthy-node-2", false)
		list := postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{primary, replica1, replica2}}
		cluster := &apiv1.Cluster{
			Spec:   apiv1.ClusterSpec{Instances: 3},
			Status: apiv1.ClusterStatus{ReadyInstances: 3},
		}

		Expect(env.clusterReconciler.shouldSetPrimaryToSchedulableNode(ctx, cluster, list, &primary)).To(BeTrue())
	})
})
