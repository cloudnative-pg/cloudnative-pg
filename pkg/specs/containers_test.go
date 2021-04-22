/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bootstrap Container creation", func() {
	It("create a Bootstrap Container with resources with nil values into Limits and Requests fields", func() {
		resources := corev1.ResourceRequirements{}
		container := createBootstrapContainer(resources, 26, 26)
		Expect(container.Resources.Limits).To(BeNil())
		Expect(container.Resources.Requests).To(BeNil())
	})

	It("create a Bootstrap Container with resources with not nil values into Limits and Requests fields", func() {
		limits := make(corev1.ResourceList)
		limits["a_test_field"] = resource.Quantity{}
		requests := make(corev1.ResourceList)
		requests["another_test_field"] = resource.Quantity{}
		resources := corev1.ResourceRequirements{Limits: limits, Requests: requests}
		container := createBootstrapContainer(resources, 26, 26)
		Expect(container.Resources.Limits["a_test_field"]).ToNot(BeNil())
		Expect(container.Resources.Requests["another_test_field"]).ToNot(BeNil())
	})
})

var _ = Describe("Container Security Context creation", func() {
	It("create a Security Context for the Container", func() {
		postgresUID := int64(1001)
		postgresGID := int64(26)
		securityContext := CreateContainerSecurityContext(postgresUID, postgresGID)
		Expect(*securityContext.RunAsNonRoot).To(BeTrue())
		Expect(*securityContext.AllowPrivilegeEscalation).To(BeFalse())
		Expect(*securityContext.Privileged).To(BeFalse())
		Expect(*securityContext.ReadOnlyRootFilesystem).To(BeFalse())
	})
})
