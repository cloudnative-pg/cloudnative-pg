/*
Copyright The CloudNativePG Contributors

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

package specs

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
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
				LogLevel: "info",
			},
		}
		container := createBootstrapContainer(cluster)
		Expect(container.Resources.Limits["a_test_field"]).ToNot(BeNil())
		Expect(container.Resources.Requests["another_test_field"]).ToNot(BeNil())
	})
})

var _ = Describe("Container Security Context creation", func() {
	It("with nil SeccompProfile", func() {
		cluster := &apiv1.Cluster{}
		utils.SetSeccompSupport(false)
		securityContext := CreateContainerSecurityContext(cluster.GetSeccompProfile())

		Expect(*securityContext.RunAsNonRoot).To(BeTrue())
		Expect(*securityContext.AllowPrivilegeEscalation).To(BeFalse())
		Expect(*securityContext.Privileged).To(BeFalse())
		Expect(*securityContext.ReadOnlyRootFilesystem).To(BeTrue())

		Expect(securityContext.SeccompProfile).To(BeNil())
	})

	It("with valid SeccompProfile", func() {
		cluster := &apiv1.Cluster{}
		runtimeProfile := &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		}
		utils.SetSeccompSupport(true)
		securityContext := CreateContainerSecurityContext(cluster.GetSeccompProfile())

		Expect(securityContext.SeccompProfile).ToNot(BeNil())
		Expect(securityContext.SeccompProfile).To(BeEquivalentTo(runtimeProfile))
	})

	It("with defined SeccompProfile", func() {
		profilePath := "/path/to/profile"
		localhostProfile := &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: &profilePath,
		}
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{
			SeccompProfile: localhostProfile,
		}}

		utils.SetSeccompSupport(true)
		securityContext := CreateContainerSecurityContext(cluster.GetSeccompProfile())
		Expect(securityContext.SeccompProfile).ToNot(BeNil())
		Expect(securityContext.SeccompProfile).To(BeEquivalentTo(localhostProfile))
		Expect(securityContext.SeccompProfile.LocalhostProfile).To(BeEquivalentTo(&profilePath))
	})
})
