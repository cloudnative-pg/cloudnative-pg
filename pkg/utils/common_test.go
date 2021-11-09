/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
)

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

var _ = Describe("Annotation management", func() {
	config := &configuration.Data{}
	config.ReadConfigMap(map[string]string{"INHERITED_ANNOTATIONS": "one,two"})

	It("must inherit annotations to be inherited", func() {
		pod := &corev1.Pod{}
		InheritAnnotations(&pod.ObjectMeta, map[string]string{"one": "1", "two": "2", "three": "3"}, config)
		Expect(pod.Annotations).To(Equal(map[string]string{"one": "1", "two": "2"}))
	})
})

var _ = Describe("Label management", func() {
	config := &configuration.Data{}
	config.ReadConfigMap(map[string]string{"INHERITED_LABELS": "alpha,beta"})

	It("must inherit labels to be inherited", func() {
		pod := &corev1.Pod{}
		InheritLabels(&pod.ObjectMeta, map[string]string{"alpha": "1", "beta": "2", "gamma": "3"}, config)
		Expect(pod.Labels).To(Equal(map[string]string{"alpha": "1", "beta": "2"}))
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
