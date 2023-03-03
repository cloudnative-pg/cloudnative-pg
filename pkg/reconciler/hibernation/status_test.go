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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hibernation annotation management", func() {
	It("classifies clusters with not annotation as not hibernated", func() {
		cluster := apiv1.Cluster{}
		Expect(getHibernationAnnotationValue(&cluster)).To(BeFalse())
	})

	It("correctly handle on/off values", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOn,
				},
			},
		}
		Expect(getHibernationAnnotationValue(&cluster)).To(BeTrue())

		cluster.ObjectMeta.Annotations[HibernationAnnotationName] = HibernationOff
		Expect(getHibernationAnnotationValue(&cluster)).To(BeFalse())
	})

	It("fails when the value of the annotation is not correct", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: "not-correct",
				},
			},
		}
		_, err := getHibernationAnnotationValue(&cluster)
		Expect(err).ToNot(Succeed())
	})
})

var _ = Describe("Status enrichment", func() {
	It("doesn't add a condition if hibernation has not been requested", func(ctx SpecContext) {
		cluster := apiv1.Cluster{}
		EnrichStatus(ctx, &cluster, nil)
		Expect(cluster.Status.Conditions).To(BeEmpty())
	})

	It("adds an error condition when the hibernation annotation have a wrong value", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: "not-correct",
				},
			},
		}
		EnrichStatus(ctx, &cluster, nil)

		hibernationCondition := meta.FindStatusCondition(cluster.Status.Conditions, HibernationConditionType)
		Expect(hibernationCondition).ToNot(BeNil())
		Expect(hibernationCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(hibernationCondition.Reason).To(Equal(HibernationConditionReasonWrongAnnotationValue))
	})

	It("removes the hibernation condition when not requested", func(ctx SpecContext) {
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
					},
				},
			},
		}

		EnrichStatus(ctx, &cluster, nil)
		hibernationCondition := meta.FindStatusCondition(cluster.Status.Conditions, HibernationConditionType)
		Expect(hibernationCondition).To(BeNil())
	})

	It("set the cluster as hibernated when every Pod have been deleted", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOn,
				},
			},
		}

		EnrichStatus(ctx, &cluster, nil)
		hibernationCondition := meta.FindStatusCondition(cluster.Status.Conditions, HibernationConditionType)
		Expect(hibernationCondition).ToNot(BeNil())
		Expect(hibernationCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(hibernationCondition.Reason).To(Equal(HibernationConditionReasonHibernated))
	})

	It("set the cluster as not hibernated when at least Pod is still alive", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOn,
				},
			},
		}

		EnrichStatus(ctx, &cluster, []corev1.Pod{{}})
		hibernationCondition := meta.FindStatusCondition(cluster.Status.Conditions, HibernationConditionType)
		Expect(hibernationCondition).ToNot(BeNil())
		Expect(hibernationCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(hibernationCondition.Reason).To(Equal(HibernationConditionReasonDeletingPods))
	})

	It("waits for Pod to be deleted gracefully", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					HibernationAnnotationName: HibernationOn,
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
		EnrichStatus(ctx, &cluster, pods)

		hibernationCondition := meta.FindStatusCondition(cluster.Status.Conditions, HibernationConditionType)
		Expect(hibernationCondition).ToNot(BeNil())
		Expect(hibernationCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(hibernationCondition.Reason).To(Equal(HibernationConditionReasonWaitingPodsDeletion))
	})
})
