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

package resize

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResizeDecisionEngine", Label("resizing"), func() {
	var (
		cluster   *apiv1.Cluster
		oldSpec   *corev1.ResourceRequirements
		newSpec   *corev1.ResourceRequirements
		engine    *DecisionEngine
		maxInc100 = int32(100)
		maxInc50  = int32(50)
		maxDec25  = int32(25)
		maxDec50  = int32(50)
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 3,
			},
		}

		oldSpec = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		}

		newSpec = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1500m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3000m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		}

		engine = NewDecisionEngine(cluster, oldSpec, newSpec)
	})

	Describe("NewDecisionEngine", func() {
		It("should create a new decision engine", func() {
			Expect(engine).NotTo(BeNil())
			Expect(engine.cluster).To(Equal(cluster))
			Expect(engine.oldSpec).To(Equal(oldSpec))
			Expect(engine.newSpec).To(Equal(newSpec))
		})
	})

	Describe("DetermineStrategy", func() {
		Context("when no resize policy is configured", func() {
			It("should default to rolling update", func() {
				strategy, err := engine.DetermineStrategy()
				Expect(err).NotTo(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})
		})

		Context("when InPlace strategy is configured", func() {
			BeforeEach(func() {
				cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
					Strategy: apiv1.ResourceResizeStrategyInPlace,
					CPU: &apiv1.ContainerResizePolicy{
						RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
						MaxIncreasePct: &maxInc100,
						MaxDecreasePct: &maxDec50,
					},
				}
			})

			It("should return InPlace when feasible", func() {
				strategy, err := engine.DetermineStrategy()
				Expect(err).NotTo(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyInPlace))
			})

			It("should fallback to rolling update when not feasible", func() {
				// Change memory to make it not CPU-only
				newSpec.Requests[corev1.ResourceMemory] = resource.MustParse("3Gi")
				newSpec.Limits[corev1.ResourceMemory] = resource.MustParse("6Gi")

				strategy, err := engine.DetermineStrategy()
				Expect(err).To(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})
		})

		Context("when RollingUpdate strategy is configured", func() {
			BeforeEach(func() {
				cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
					Strategy: apiv1.ResourceResizeStrategyRollingUpdate,
				}
			})

			It("should always return rolling update", func() {
				strategy, err := engine.DetermineStrategy()
				Expect(err).NotTo(HaveOccurred())
				Expect(strategy).To(Equal(apiv1.ResourceResizeStrategyRollingUpdate))
			})
		})

	})

	Describe("canResizeInPlace", func() {
		BeforeEach(func() {
			cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
				Strategy: apiv1.ResourceResizeStrategyInPlace,
				CPU: &apiv1.ContainerResizePolicy{
					RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
					MaxIncreasePct: &maxInc100,
					MaxDecreasePct: &maxDec50,
				},
			}
		})

		It("should return true for CPU-only changes within thresholds", func() {
			result := engine.canResizeInPlace()
			Expect(result).To(BeTrue())
		})

		It("should return false for memory changes", func() {
			newSpec.Requests[corev1.ResourceMemory] = resource.MustParse("3Gi")
			newSpec.Limits[corev1.ResourceMemory] = resource.MustParse("6Gi")

			result := engine.canResizeInPlace()
			Expect(result).To(BeFalse())
		})

		It("should return false when CPU increase exceeds threshold", func() {
			cluster.Spec.ResourceResizePolicy.CPU.MaxIncreasePct = &maxInc50
			// Current change is 50% increase (1000m to 1500m), which should be at the limit

			result := engine.canResizeInPlace()
			Expect(result).To(BeTrue())

			// Now make it exceed the threshold
			newSpec.Requests[corev1.ResourceCPU] = resource.MustParse("1600m")
			newSpec.Limits[corev1.ResourceCPU] = resource.MustParse("3200m")

			result = engine.canResizeInPlace()
			Expect(result).To(BeFalse())
		})
	})

	Describe("calculateResourceChange", func() {
		It("should calculate percentage increase correctly", func() {
			change := engine.calculateResourceChange(corev1.ResourceCPU)
			Expect(change).To(BeNumerically("~", 50.0, 0.1)) // 50% increase
		})

		It("should calculate percentage decrease correctly", func() {
			// Decrease CPU
			newSpec.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
			newSpec.Limits[corev1.ResourceCPU] = resource.MustParse("1000m")

			change := engine.calculateResourceChange(corev1.ResourceCPU)
			Expect(change).To(BeNumerically("~", -50.0, 0.1)) // 50% decrease
		})

		It("should return 0 for no change", func() {
			newSpec.Requests[corev1.ResourceCPU] = resource.MustParse("1000m")
			newSpec.Limits[corev1.ResourceCPU] = resource.MustParse("2000m")

			change := engine.calculateResourceChange(corev1.ResourceCPU)
			Expect(change).To(BeNumerically("~", 0.0, 0.1))
		})

		It("should handle zero old value", func() {
			oldSpec.Requests = corev1.ResourceList{}
			oldSpec.Limits = corev1.ResourceList{}

			change := engine.calculateResourceChange(corev1.ResourceCPU)
			Expect(change).To(Equal(100.0)) // Treat as 100% increase
		})
	})

	Describe("isCPUOnlyChange", func() {
		It("should return true when only CPU changes", func() {
			result := engine.isCPUOnlyChange()
			Expect(result).To(BeTrue())
		})

		It("should return false when memory also changes", func() {
			newSpec.Requests[corev1.ResourceMemory] = resource.MustParse("3Gi")
			newSpec.Limits[corev1.ResourceMemory] = resource.MustParse("6Gi")

			result := engine.isCPUOnlyChange()
			Expect(result).To(BeFalse())
		})
	})

	Describe("isWithinThresholds", func() {
		BeforeEach(func() {
			cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
				CPU: &apiv1.ContainerResizePolicy{
					MaxIncreasePct: &maxInc100,
					MaxDecreasePct: &maxDec25,
				},
			}
		})

		It("should return true when within CPU increase threshold", func() {
			result := engine.isWithinThresholds()
			Expect(result).To(BeTrue())
		})

		It("should return false when exceeding CPU increase threshold", func() {
			cluster.Spec.ResourceResizePolicy.CPU.MaxIncreasePct = &maxDec25 // 25%
			// Current change is 50%, which exceeds 25%

			result := engine.isWithinThresholds()
			Expect(result).To(BeFalse())
		})

		It("should return false when exceeding CPU decrease threshold", func() {
			// Make a 50% decrease
			newSpec.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
			newSpec.Limits[corev1.ResourceCPU] = resource.MustParse("1000m")

			result := engine.isWithinThresholds()
			Expect(result).To(BeFalse()) // 50% decrease exceeds 25% threshold
		})
	})

})
