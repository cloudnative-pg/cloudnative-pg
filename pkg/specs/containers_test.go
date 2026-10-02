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

package specs

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bootstrap Container creation", func() {
	It("create a Bootstrap Container with resources with nil values into Limits and Requests fields", func() {
		cluster := apiv1.Cluster{}
		container := createBootstrapContainer(cluster, nil)
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
		container := createBootstrapContainer(cluster, nil)
		Expect(container.Resources.Limits["a_test_field"]).ToNot(BeNil())
		Expect(container.Resources.Requests["another_test_field"]).ToNot(BeNil())
	})
})

var _ = Describe("GetSecurityContext", func() {
	It("returns defaults when Spec.SecurityContext is nil", func() {
		cluster := &apiv1.Cluster{}
		sc := GetSecurityContext(cluster)

		Expect(sc).ToNot(BeNil())
		Expect(sc.SeccompProfile).ToNot(BeNil())
		Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		Expect(sc.RunAsUser).To(BeNil())
		Expect(sc.RunAsGroup).To(BeNil())
		Expect(sc.Capabilities).ToNot(BeNil())
		Expect(sc.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
		Expect(sc.Privileged).ToNot(BeNil())
		Expect(*sc.Privileged).To(BeFalse())
		Expect(sc.RunAsNonRoot).ToNot(BeNil())
		Expect(*sc.RunAsNonRoot).To(BeTrue())
		Expect(sc.ReadOnlyRootFilesystem).ToNot(BeNil())
		Expect(*sc.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(sc.AllowPrivilegeEscalation).ToNot(BeNil())
		Expect(*sc.AllowPrivilegeEscalation).To(BeFalse())
	})

	It("merges provided partial SecurityContext with defaults", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To(int64(1000)),
				},
			},
		}

		sc := GetSecurityContext(cluster)
		Expect(sc.RunAsUser).ToNot(BeNil())
		Expect(*sc.RunAsUser).To(Equal(int64(1000)))
		Expect(sc.RunAsGroup).To(BeNil())
		Expect(sc.Capabilities).ToNot(BeNil())
		Expect(sc.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
		Expect(sc.Privileged).ToNot(BeNil())
		Expect(*sc.Privileged).To(BeFalse())
		Expect(sc.RunAsNonRoot).ToNot(BeNil())
		Expect(*sc.RunAsNonRoot).To(BeTrue())
		Expect(sc.ReadOnlyRootFilesystem).ToNot(BeNil())
		Expect(*sc.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(sc.AllowPrivilegeEscalation).ToNot(BeNil())
		Expect(*sc.AllowPrivilegeEscalation).To(BeFalse())
	})

	It("honors Cluster.Spec.SeccompProfile for container security context", func() {
		profilePath := "/path/to/container/profile"
		localhostProfile := &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: &profilePath,
		}
		cluster := &apiv1.Cluster{Spec: apiv1.ClusterSpec{SeccompProfile: localhostProfile}}

		sc := GetSecurityContext(cluster)
		Expect(sc.SeccompProfile).ToNot(BeNil())
		Expect(sc.SeccompProfile).To(BeEquivalentTo(localhostProfile))
		Expect(sc.SeccompProfile.LocalhostProfile).To(BeEquivalentTo(&profilePath))
	})

	It("does not override non-nil Capabilities, even if empty", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{},
				},
			},
		}

		sc := GetSecurityContext(cluster)
		Expect(sc.Capabilities).ToNot(BeNil())
		Expect(sc.Capabilities.Drop).To(BeNil())
		Expect(sc.RunAsUser).To(BeNil())
		Expect(sc.RunAsGroup).To(BeNil())
	})

	It("preserves boolean and capabilities settings provided by the user", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				SecurityContext: &corev1.SecurityContext{
					Privileged:               ptr.To(true),
					RunAsNonRoot:             ptr.To(false),
					ReadOnlyRootFilesystem:   ptr.To(false),
					AllowPrivilegeEscalation: ptr.To(true),
					Capabilities: &corev1.Capabilities{
						Add:  []corev1.Capability{"NET_BIND_SERVICE"},
						Drop: []corev1.Capability{"MKNOD"},
					},
				},
			},
		}

		sc := GetSecurityContext(cluster)
		Expect(sc.Privileged).ToNot(BeNil())
		Expect(*sc.Privileged).To(BeTrue())
		Expect(sc.RunAsNonRoot).ToNot(BeNil())
		Expect(*sc.RunAsNonRoot).To(BeFalse())
		Expect(sc.ReadOnlyRootFilesystem).ToNot(BeNil())
		Expect(*sc.ReadOnlyRootFilesystem).To(BeFalse())
		Expect(sc.AllowPrivilegeEscalation).ToNot(BeNil())
		Expect(*sc.AllowPrivilegeEscalation).To(BeTrue())
		Expect(sc.Capabilities).ToNot(BeNil())
		Expect(sc.Capabilities.Add).To(ContainElement(corev1.Capability("NET_BIND_SERVICE")))
		Expect(sc.Capabilities.Drop).To(ContainElement(corev1.Capability("MKNOD")))
	})
})
