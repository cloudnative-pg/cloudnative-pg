/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package controllers

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

var _ = Describe("Check pods not on primary node", func() {
	item1 := postgres.PostgresqlStatus{
		IsPrimary: false,
		Node:      "node-1",
		Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
	}

	item2 := postgres.PostgresqlStatus{
		IsPrimary: false,
		Node:      "node-2",
		Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
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
