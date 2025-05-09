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

package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeInhericanceController struct {
	labels      []string
	annotations []string
}

func (ctrl *fakeInhericanceController) IsLabelInherited(key string) bool {
	return slices.Contains(ctrl.labels, key)
}

func (ctrl *fakeInhericanceController) IsAnnotationInherited(key string) bool {
	return slices.Contains(ctrl.annotations, key)
}

var _ = Describe("Operator version annotation management", func() {
	pod := corev1.Pod{}
	podTwo := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"test": "toast",
			},
		},
	}

	It("must annotate empty objects", func() {
		SetOperatorVersion(&pod.ObjectMeta, "2.3.2")
		Expect(pod.ObjectMeta.Annotations[OperatorVersionAnnotationName]).To(Equal("2.3.2"))
	})

	It("must not forget existing annotations", func() {
		SetOperatorVersion(&podTwo.ObjectMeta, "2.3.3")
		Expect(podTwo.ObjectMeta.Annotations[OperatorVersionAnnotationName]).To(Equal("2.3.3"))
		Expect(podTwo.ObjectMeta.Annotations["test"]).To(Equal("toast"))
	})
})

// nolint:dupl
var _ = Describe("Annotation management", func() {
	config := &fakeInhericanceController{
		annotations: []string{
			"one",
			"two",
		},
	}

	toBeMatchedMap := map[string]string{"one": "1", "two": "2", "three": "3"}
	fixedMap := map[string]string{"four": "4", "five": "5"}

	It("must inherit annotations to be inherited", func() {
		pod := &corev1.Pod{}
		InheritAnnotations(&pod.ObjectMeta, toBeMatchedMap, nil, config)
		Expect(pod.Annotations).To(Equal(map[string]string{"one": "1", "two": "2"}))
	})

	It("must inherit annotations to be inherited with fixed ones too", func() {
		pod := &corev1.Pod{}
		InheritAnnotations(&pod.ObjectMeta, toBeMatchedMap, fixedMap, config)
		Expect(pod.Annotations).To(Equal(map[string]string{"one": "1", "two": "2", "four": "4", "five": "5"}))
	})
})

// nolint:dupl
var _ = Describe("Label management", func() {
	config := &fakeInhericanceController{
		labels: []string{
			"alpha",
			"beta",
		},
	}
	toBeMatchedMap := map[string]string{"alpha": "1", "beta": "2", "gamma": "3"}
	fixedMap := map[string]string{"delta": "4", "epsilon": "5"}

	It("must inherit labels to be inherited", func() {
		pod := &corev1.Pod{}
		InheritLabels(&pod.ObjectMeta, toBeMatchedMap, nil, config)
		Expect(pod.Labels).To(Equal(map[string]string{"alpha": "1", "beta": "2"}))
	})
	It("must inherit labels to be inherited with fixed ones passed", func() {
		pod := &corev1.Pod{}
		InheritLabels(&pod.ObjectMeta, toBeMatchedMap,
			fixedMap, config)
		Expect(pod.Labels).To(Equal(map[string]string{"alpha": "1", "beta": "2", "delta": "4", "epsilon": "5"}))
	})
})

var _ = Describe("Label cluster name management", func() {
	pod := corev1.Pod{}
	podTwo := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "toast",
			},
		},
	}

	It("must label empty objects", func() {
		LabelClusterName(&pod.ObjectMeta, "test-label")
		Expect(pod.ObjectMeta.Labels[ClusterLabelName]).To(Equal("test-label"))
	})

	It("must not forget existing labels", func() {
		LabelClusterName(&podTwo.ObjectMeta, "test-label")
		Expect(podTwo.ObjectMeta.Labels[ClusterLabelName]).To(Equal("test-label"))
		Expect(podTwo.ObjectMeta.Labels["test"]).To(Equal("toast"))
	})
})

var _ = Describe("Annotate pods management", func() {
	const appArmorPostgres = AppArmorAnnotationPrefix + "/postgres"
	annotations := map[string]string{
		appArmorPostgres: "unconfined",
	}

	It("must annotate empty objects with the matching container", func() {
		pod := corev1.Pod{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres"}}},
		}

		AnnotateAppArmor(&pod.ObjectMeta, &pod.Spec, annotations)
		Expect(pod.ObjectMeta.Annotations[appArmorPostgres]).To(Equal("unconfined"))
	})

	It("must not annotate the object if the container is not present", func() {
		pod := corev1.Pod{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "test"}}},
		}

		AnnotateAppArmor(&pod.ObjectMeta, &pod.Spec, annotations)
		_, isPresent := pod.Annotations[appArmorPostgres]
		Expect(isPresent).To(BeFalse())
	})
})

var _ = Describe("Pod spec reconciliation", func() {
	var objectMeta *metav1.ObjectMeta
	BeforeEach(func() {
		objectMeta = &metav1.ObjectMeta{Annotations: map[string]string{}}
	})

	It("is not disabled if annotation map is empty", func() {
		Expect(IsPodSpecReconciliationDisabled(objectMeta)).To(BeFalse())
	})

	It("is not disabled if annotation exists and its value is not 'disabled'", func() {
		objectMeta.Annotations[ReconcilePodSpecAnnotationName] = string(annotationStatusEnabled)
		Expect(IsPodSpecReconciliationDisabled(objectMeta)).To(BeFalse())
	})

	It("is disabled if annotation exists and its value is 'disabled'", func() {
		objectMeta.Annotations[ReconcilePodSpecAnnotationName] = string(annotationStatusDisabled)
		Expect(IsPodSpecReconciliationDisabled(objectMeta)).To(BeTrue())
	})
})
