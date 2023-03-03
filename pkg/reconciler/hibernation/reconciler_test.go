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

package hibernation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type clientMock struct {
	client.Reader
	client.Writer
	client.StatusClient
	client.SubResourceClientConstructor

	deletedPods []string
}

func (cm *clientMock) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	cm.deletedPods = append(cm.deletedPods, obj.GetName())
	return nil
}

func (cm *clientMock) Scheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (cm *clientMock) RESTMapper() meta.RESTMapper {
	return nil
}

var _ = Describe("Reconcile resources", func() {
	It("let the reconciliation loop proceed when there's no hibernation annotation", func(ctx SpecContext) {
		mock := &clientMock{}
		cluster := apiv1.Cluster{}
		Expect(Reconcile(ctx, mock, &cluster, nil)).To(BeZero())
		Expect(mock.deletedPods).To(BeEmpty())
	})

	It("let the reconciliation loog stop if the cluster is already hibernated", func(ctx SpecContext) {
		mock := &clientMock{}
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOff,
				},
			},
			Status: apiv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   HibernationConditionType,
						Status: metav1.ConditionTrue,
						Reason: HibernationConditionReasonHibernated,
					},
				},
			},
		}
		Expect(Reconcile(ctx, mock, &cluster, nil)).ToNot(BeZero())
		Expect(mock.deletedPods).To(BeEmpty())
	})

	It("waits if a Pod is being deleted", func(ctx SpecContext) {
		mock := &clientMock{}
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOff,
				},
			},
			Status: apiv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   HibernationConditionType,
						Status: metav1.ConditionTrue,
						Reason: HibernationConditionReasonWaitingPodsDeletion,
					},
				},
			},
		}

		now := metav1.Now()
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
				},
			},
		}
		Expect(Reconcile(ctx, mock, &cluster, pods)).ToNot(BeZero())
		Expect(mock.deletedPods).To(BeEmpty())
	})

	It("delete the primary pod if available", func(ctx SpecContext) {
		mock := &clientMock{}
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOff,
				},
				Name: "cluster-example",
			},
			Status: apiv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   HibernationConditionType,
						Status: metav1.ConditionTrue,
						Reason: HibernationConditionReasonDeletingPods,
					},
				},
			},
		}

		pods := fakePodListWithPrimary()
		Expect(Reconcile(ctx, mock, &cluster, pods)).ToNot(BeZero())
		Expect(mock.deletedPods).To(ConsistOf("cluster-example-2"))
	})

	It("delete the replicas if the primary pod have already been deleted", func(ctx SpecContext) {
		mock := &clientMock{}
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOff,
				},
				Name: "cluster-example",
			},
			Status: apiv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   HibernationConditionType,
						Status: metav1.ConditionTrue,
						Reason: HibernationConditionReasonDeletingPods,
					},
				},
			},
		}

		pods := fakePodListWithoutPrimary()
		Expect(Reconcile(ctx, mock, &cluster, pods)).ToNot(BeZero())
		Expect(mock.deletedPods).To(ConsistOf("cluster-example-1"))
	})
})

func fakePod(name string, role string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				specs.ClusterRoleLabelName: role,
			},
		},
	}
}

func fakePodListWithoutPrimary() []corev1.Pod {
	return []corev1.Pod{
		fakePod("cluster-example-1", specs.ClusterRoleLabelReplica),
		fakePod("cluster-example-2", specs.ClusterRoleLabelReplica),
	}
}

func fakePodListWithPrimary() []corev1.Pod {
	return []corev1.Pod{
		fakePod("cluster-example-1", specs.ClusterRoleLabelReplica),
		fakePod("cluster-example-2", specs.ClusterRoleLabelPrimary),
		fakePod("cluster-example-3", specs.ClusterRoleLabelReplica),
	}
}
