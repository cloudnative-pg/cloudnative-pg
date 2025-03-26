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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Base type mappings for secrets", func() {
	It("correctly map nil values", func() {
		Expect(SecretKeySelectorToCore(nil)).To(BeNil())
	})

	It("correctly map non-nil values", func() {
		selector := SecretKeySelector{
			LocalObjectReference: LocalObjectReference{
				Name: "thisName",
			},
			Key: "thisKey",
		}

		Expect(selector.Name).To(Equal("thisName"))
		Expect(selector.Key).To(Equal("thisKey"))
	})
})

var _ = Describe("Base type mappings for configmaps", func() {
	It("correctly map nil values", func() {
		Expect(ConfigMapKeySelectorToCore(nil)).To(BeNil())
	})

	It("correctly map non-nil values", func() {
		selector := ConfigMapKeySelector{
			LocalObjectReference: LocalObjectReference{
				Name: "thisName",
			},
			Key: "thisKey",
		}

		Expect(selector.Name).To(Equal("thisName"))
		Expect(selector.Key).To(Equal("thisKey"))
	})
})

var _ = Describe("Properly builds ListStatusPods", func() {
	healthyPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "healthyPod",
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
	activePod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "activePod",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	failedPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "failedPod",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	now := metav1.Now()
	terminatingPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "terminatingPod",
			DeletionTimestamp: &now,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	It("Detects healthy pods", func() {
		podList := []corev1.Pod{healthyPod, healthyPod}
		expectedStatus := map[PodStatus][]string{
			PodHealthy: {"healthyPod", "healthyPod"},
		}
		podStatus := ListStatusPods(podList)
		Expect(podStatus).To(BeEquivalentTo(expectedStatus))
	})

	It("Detects active pods", func() {
		podList := []corev1.Pod{healthyPod, activePod}
		expectedStatus := map[PodStatus][]string{
			PodHealthy:     {"healthyPod"},
			PodReplicating: {"activePod"},
		}
		podStatus := ListStatusPods(podList)
		Expect(podStatus).To(BeEquivalentTo(expectedStatus))
	})

	It("Detects failed pods", func() {
		podList := []corev1.Pod{healthyPod, activePod, failedPod}
		expectedStatus := map[PodStatus][]string{
			PodHealthy:     {"healthyPod"},
			PodReplicating: {"activePod"},
			PodFailed:      {"failedPod"},
		}
		podStatus := ListStatusPods(podList)
		Expect(podStatus).To(BeEquivalentTo(expectedStatus))
	})

	It("Excludes terminating pods", func() {
		podList := []corev1.Pod{healthyPod, activePod, failedPod, terminatingPod}
		expectedStatus := map[PodStatus][]string{
			PodHealthy:     {"healthyPod"},
			PodReplicating: {"activePod"},
			PodFailed:      {"failedPod"},
		}
		podStatus := ListStatusPods(podList)
		Expect(podStatus).To(BeEquivalentTo(expectedStatus))
	})
})
