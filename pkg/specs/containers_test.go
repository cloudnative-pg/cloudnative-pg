/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bootstrap Container creation", func() {
	It("create a Bootstrap Container with resources with nil values into Limits and Requests fields", func() {
		cluster := apiv1.Cluster{}
		container := createBootstrapContainer(cluster)
		Expect(container.Resources.Limits).To(BeNil())
		Expect(container.Resources.Requests).To(BeNil())
	})

	It("create a Bootstrap Container with resources with not nil values into Limits and Requests fields", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"a_test_field": resource.Quantity{},
					},
					Requests: corev1.ResourceList{
						"another_test_field": resource.Quantity{},
					},
				},
			},
		}
		container := createBootstrapContainer(cluster)
		Expect(container.Resources.Limits["a_test_field"]).ToNot(BeNil())
		Expect(container.Resources.Requests["another_test_field"]).ToNot(BeNil())
	})
})

var _ = Describe("Container Security Context creation", func() {
	It("create a Security Context for the Container", func() {
		securityContext := CreateContainerSecurityContext()
		Expect(*securityContext.RunAsNonRoot).To(BeTrue())
		Expect(*securityContext.AllowPrivilegeEscalation).To(BeFalse())
		Expect(*securityContext.Privileged).To(BeFalse())
		Expect(*securityContext.ReadOnlyRootFilesystem).To(BeTrue())
	})
})
