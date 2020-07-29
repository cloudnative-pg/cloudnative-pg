/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Primary instance detection", func() {
	car1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "car-1",
			Namespace: "default",
			Annotations: map[string]string{
				specs.ClusterSerialAnnotationName: "1",
			},
			Labels: map[string]string{
				specs.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary,
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
				specs.ClusterSerialAnnotationName: "2",
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
				specs.ClusterSerialAnnotationName: "3",
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
				specs.ClusterSerialAnnotationName: "4",
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

	foobar := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foobar",
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

	It("detects no primary if the list of Pods is empty", func() {
		var podList []corev1.Pod
		Expect(getPrimaryPod(podList)).To(BeNil())
	})

	It("detects no primary if we have not a ready Pod", func() {
		podList := []corev1.Pod{foo, bar}
		Expect(getPrimaryPod(podList)).To(BeNil())
	})

	It("detects the primary if it is the first available", func() {
		podList := []corev1.Pod{foo, bar, car1, car2}
		result := getPrimaryPod(podList)
		Expect(result).ToNot(BeNil())
		Expect(result.Name).To(Equal("car-1"))
	})

	It("detects the primary if it is not the first one", func() {
		podList := []corev1.Pod{car2, foo, bar, car1}
		result := getPrimaryPod(podList)
		Expect(result).ToNot(BeNil())
		Expect(result.Name).To(Equal("car-1"))
	})

	It("we don't own the container", func() {
		podList := []corev1.Pod{foobar}
		Expect(getPrimaryPod(podList)).To(BeNil())
	})
})

var _ = Describe("Sacrificial Pod detection", func() {
	car1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "car-1",
			Namespace: "default",
			Annotations: map[string]string{
				specs.ClusterSerialAnnotationName: "1",
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
				specs.ClusterSerialAnnotationName: "2",
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
				specs.ClusterSerialAnnotationName: "3",
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
				specs.ClusterSerialAnnotationName: "4",
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
		Expect(getSacrificialPod(podList)).To(BeNil())
	})

	It("detects if we have not a ready Pod", func() {
		podList := []corev1.Pod{foo, bar}
		Expect(getSacrificialPod(podList)).To(BeNil())
	})

	It("detects it if is the first available", func() {
		podList := []corev1.Pod{foo, bar, car1, car2}
		result := getSacrificialPod(podList)
		Expect(result).ToNot(BeNil())
		Expect(result.Name).To(Equal("car-2"))
	})

	It("detects it if is not the first one", func() {
		podList := []corev1.Pod{car2, foo, bar, car1}
		result := getSacrificialPod(podList)
		Expect(result).ToNot(BeNil())
		Expect(result.Name).To(Equal("car-2"))
	})
})
