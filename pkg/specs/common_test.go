/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
