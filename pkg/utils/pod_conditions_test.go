/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod conditions test suite", func() {
	Describe("Must check for Running PODs", func() {
		It("Detect PODs without conditions are not running", func() {
			var pod = corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
				},
			}
			Expect(IsPodReady(pod)).To(BeFalse())
		})

		It("Detects Ready PODs as running", func() {
			var pod = corev1.Pod{
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
			var pod = corev1.Pod{
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

	Describe("Must check for Pods which are being upgraded", func() {
		alignedPod := corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "postgres",
						Image: "postgres:12.1",
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "postgres",
						Image: "postgres:12.1",
					},
				},
			},
		}

		upgradingPod := corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "postgres",
						Image: "postgres:12.2",
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "postgres",
						Image: "postgres:12.1",
					},
				},
			},
		}

		It("classifies an empty for as not upgrading", func() {
			Expect(IsPodUpgrading(corev1.Pod{})).To(BeFalse())
		})

		It("classifies as not upgrading an aligned Pod", func() {
			Expect(IsPodUpgrading(alignedPod)).To(BeFalse())
		})

		It("classifies as upgrading a Pod which is being upgraded", func() {
			Expect(IsPodUpgrading(upgradingPod)).To(BeTrue())
		})

		It("classifies as upgrading a Pod which is missing a requested container", func() {
			podMissingContainer := corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: "postgres:12.2",
						},
					},
				},
				Status: corev1.PodStatus{},
			}

			Expect(IsPodUpgrading(podMissingContainer)).To(BeTrue())
		})

		It("classified as upgrading a Pod which have a container to be terminated", func() {
			podWithTerminatingContainer := corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "postgres",
							Image: "postgres:12.1",
						},
					},
				},
			}

			Expect(IsPodUpgrading(podWithTerminatingContainer)).To(BeTrue())
		})

		It("counts pods being upgraded in a list", func() {
			podList := []corev1.Pod{
				upgradingPod,
				alignedPod,
			}

			Expect(CountUpgradingPods(podList)).To(Equal(1))
		})
	})
})
