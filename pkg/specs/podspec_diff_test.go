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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodSpecDiff", func() {
	It("return false when the startup probe do not match and true otherwise", func() {
		containerPre := corev1.Container{
			StartupProbe: &corev1.Probe{
				TimeoutSeconds: 23,
			},
		}
		containerPost := corev1.Container{
			StartupProbe: &corev1.Probe{
				TimeoutSeconds: 24,
			},
		}
		Expect(doContainersMatch(containerPre, containerPre)).To(BeTrue())
		status, diff := doContainersMatch(containerPre, containerPost)
		Expect(status).To(BeFalse())
		Expect(diff).To(Equal("startup-probe"))
	})

	It("return false when the liveness probe do not match and true otherwise", func() {
		containerPre := corev1.Container{
			LivenessProbe: &corev1.Probe{
				InitialDelaySeconds: 23,
			},
		}
		containerPost := corev1.Container{
			LivenessProbe: &corev1.Probe{
				InitialDelaySeconds: 24,
			},
		}
		Expect(doContainersMatch(containerPre, containerPre)).To(BeTrue())
		status, diff := doContainersMatch(containerPre, containerPost)
		Expect(status).To(BeFalse())
		Expect(diff).To(Equal("liveness-probe"))
	})

	It("return false when the readiness probe do not match and true otherwise", func() {
		containerPre := corev1.Container{
			ReadinessProbe: &corev1.Probe{
				SuccessThreshold: 23,
			},
		}
		containerPost := corev1.Container{
			ReadinessProbe: &corev1.Probe{
				SuccessThreshold: 24,
			},
		}
		Expect(doContainersMatch(containerPre, containerPre)).To(BeTrue())
		status, diff := doContainersMatch(containerPre, containerPost)
		Expect(status).To(BeFalse())
		Expect(diff).To(Equal("readiness-probe"))
	})
})
