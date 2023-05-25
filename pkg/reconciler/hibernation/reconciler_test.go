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
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func (cm *clientMock) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	cm.deletedPods = append(cm.deletedPods, obj.GetName())
	return nil
}

func (cm *clientMock) Scheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (cm *clientMock) RESTMapper() meta.RESTMapper {
	return nil
}

func (cm *clientMock) GroupVersionKindFor(_ runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}

func (cm *clientMock) IsObjectNamespaced(_ runtime.Object) (bool, error) {
	return false, nil
}

var _ = Describe("Reconcile resources", func() {
	It("lets the reconciliation loop proceed when there's no hibernation annotation", func(ctx SpecContext) {
		mock := &clientMock{}
		cluster := apiv1.Cluster{}
		// A Reconcile with a nil return will allow cluster reconciliation to proceed
		Expect(Reconcile(ctx, mock, &cluster, nil)).To(BeNil())
		Expect(mock.deletedPods).To(BeEmpty())
	})

	It("stops the reconciliation loop if the cluster is already hibernated", func(ctx SpecContext) {
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
		// A Reconcile with a non-nil return will stop the cluster reconciliation
		Expect(Reconcile(ctx, mock, &cluster, nil)).ToNot(BeNil())
		Expect(mock.deletedPods).To(BeEmpty())
	})

	It("re-queues if a Pod is being deleted", func(ctx SpecContext) {
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
		result, err := Reconcile(ctx, mock, &cluster, pods)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.RequeueAfter).ToNot(BeZero())
		Expect(mock.deletedPods).To(BeEmpty())
	})

	It("deletes the primary pod if available", func(ctx SpecContext) {
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
		Expect(Reconcile(ctx, mock, &cluster, pods)).ToNot(BeNil())
		Expect(mock.deletedPods).To(ConsistOf("cluster-example-2"))
	})

	It("deletes the replica pods if the primary has already been deleted", func(ctx SpecContext) {
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
		Expect(Reconcile(ctx, mock, &cluster, pods)).ToNot(BeNil())
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
