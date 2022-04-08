/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod conditions test suite", func() {
	Describe("Must check for Running PODs", func() {
		It("Detect PODs without conditions are not running", func() {
			pod := corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
				},
			}
			Expect(IsPodReady(pod)).To(BeFalse())
		})

		It("Detects Ready PODs as running", func() {
			pod := corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			Expect(IsPodReady(pod)).To(BeTrue())
		})

		It("Detects not ready PODs are not running", func() {
			pod := corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			Expect(IsPodReady(pod)).To(BeFalse())
		})

		It("return 0 if the list of Pods is empty", func() {
			var list []corev1.Pod
			Expect(CountReadyPods(list)).To(Equal(0))
		})

		It("return the number of ready Pods", func() {
			car1 := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "car-1",
					Namespace: "default",
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
			podList := []corev1.Pod{car2, foo, bar, car1}

			Expect(CountReadyPods(podList)).To(Equal(2))
		})
	})

	Describe("Must detect if a pod has been evicted or not", func() {
		pod := corev1.Pod{
			Status: corev1.PodStatus{
				Phase:  corev1.PodFailed,
				Reason: PodReasonEvicted,
				Message: "The node was low on resource: memory. " +
					"Container postgres was using 111 which exceeds its request of 0.",
			},
		}
		Expect(IsPodEvicted(pod)).To(BeTrue())

		pod = corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.ContainersReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		Expect(IsPodEvicted(pod)).To(BeFalse())
	})
})
