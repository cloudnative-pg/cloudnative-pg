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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodSpec resize configuration", Label("resizing"), func() {
	var (
		podSpec   *corev1.PodSpec
		cluster   *apiv1.Cluster
		maxInc100 = int32(100)
		maxDec50  = int32(50)
	)

	BeforeEach(func() {
		podSpec = &corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  PostgresContainerName,
					Image: "postgres:15",
				},
				{
					Name:  "other-container",
					Image: "other:latest",
				},
			},
		}

		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 3,
			},
		}
	})

	Describe("UpdatePodSpecForResize", func() {
		Context("when no resize policy is configured", func() {
			It("should not modify the pod spec", func() {
				originalSpec := podSpec.DeepCopy()
				UpdatePodSpecForResize(podSpec, cluster)
				Expect(podSpec).To(Equal(originalSpec))
			})
		})

		Context("when resize policy is configured", func() {
			BeforeEach(func() {
				cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
					Strategy: apiv1.ResourceResizeStrategyAuto,
					CPU: &apiv1.ContainerResizePolicy{
						RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
						MaxIncreasePct: &maxInc100,
						MaxDecreasePct: &maxDec50,
					},
					Memory: &apiv1.ContainerResizePolicy{
						RestartPolicy:  apiv1.ContainerRestartPolicyRestartContainer,
						MaxIncreasePct: &maxDec50,
						MaxDecreasePct: int32Ptr(25),
					},
				}
			})

			It("should add resize policies to PostgreSQL container", func() {
				UpdatePodSpecForResize(podSpec, cluster)

				// Find PostgreSQL container
				var postgresContainer *corev1.Container
				for i := range podSpec.Containers {
					if podSpec.Containers[i].Name == PostgresContainerName {
						postgresContainer = &podSpec.Containers[i]
						break
					}
				}

				Expect(postgresContainer).NotTo(BeNil())
				Expect(postgresContainer.ResizePolicy).To(HaveLen(2))

				// Check CPU resize policy
				var cpuPolicy *corev1.ContainerResizePolicy
				var memoryPolicy *corev1.ContainerResizePolicy

				for i := range postgresContainer.ResizePolicy {
					switch postgresContainer.ResizePolicy[i].ResourceName {
					case corev1.ResourceCPU:
						cpuPolicy = &postgresContainer.ResizePolicy[i]
					case corev1.ResourceMemory:
						memoryPolicy = &postgresContainer.ResizePolicy[i]
					}
				}

				Expect(cpuPolicy).NotTo(BeNil())
				Expect(cpuPolicy.RestartPolicy).To(Equal(corev1.NotRequired))

				Expect(memoryPolicy).NotTo(BeNil())
				Expect(memoryPolicy.RestartPolicy).To(Equal(corev1.RestartContainer))
			})

			It("should not modify other containers", func() {
				originalOtherContainer := podSpec.Containers[1].DeepCopy()
				UpdatePodSpecForResize(podSpec, cluster)

				Expect(podSpec.Containers[1]).To(Equal(*originalOtherContainer))
			})

			It("should handle missing PostgreSQL container gracefully", func() {
				// Remove PostgreSQL container
				podSpec.Containers = []corev1.Container{
					{
						Name:  "other-container",
						Image: "other:latest",
					},
				}

				Expect(func() {
					UpdatePodSpecForResize(podSpec, cluster)
				}).NotTo(Panic())
			})
		})

		Context("when only CPU policy is configured", func() {
			BeforeEach(func() {
				cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
					Strategy: apiv1.ResourceResizeStrategyInPlace,
					CPU: &apiv1.ContainerResizePolicy{
						RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
						MaxIncreasePct: &maxInc100,
					},
				}
			})

			It("should only add CPU resize policy", func() {
				UpdatePodSpecForResize(podSpec, cluster)

				// Find PostgreSQL container
				var postgresContainer *corev1.Container
				for i := range podSpec.Containers {
					if podSpec.Containers[i].Name == PostgresContainerName {
						postgresContainer = &podSpec.Containers[i]
						break
					}
				}

				Expect(postgresContainer).NotTo(BeNil())
				Expect(postgresContainer.ResizePolicy).To(HaveLen(1))
				Expect(postgresContainer.ResizePolicy[0].ResourceName).To(Equal(corev1.ResourceCPU))
				Expect(postgresContainer.ResizePolicy[0].RestartPolicy).To(Equal(corev1.NotRequired))
			})
		})

		Context("when container already has resize policies", func() {
			BeforeEach(func() {
				podSpec.Containers[0].ResizePolicy = []corev1.ContainerResizePolicy{
					{
						ResourceName:  corev1.ResourceEphemeralStorage,
						RestartPolicy: corev1.NotRequired,
					},
				}

				cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
					CPU: &apiv1.ContainerResizePolicy{
						RestartPolicy: apiv1.ContainerRestartPolicyNotRequired,
					},
				}
			})

			It("should append new resize policies", func() {
				UpdatePodSpecForResize(podSpec, cluster)

				postgresContainer := &podSpec.Containers[0]
				Expect(postgresContainer.ResizePolicy).To(HaveLen(2))

				// Original policy should still be there
				found := false
				for _, policy := range postgresContainer.ResizePolicy {
					if policy.ResourceName == corev1.ResourceEphemeralStorage {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue())
			})
		})
	})

	Describe("ShouldEnableResizePolicy", func() {
		It("should return false when no resize policy is configured", func() {
			result := ShouldEnableResizePolicy(cluster)
			Expect(result).To(BeFalse())
		})

		It("should return true when resize policy is configured", func() {
			cluster.Spec.ResourceResizePolicy = &apiv1.ResourceResizePolicy{
				Strategy: apiv1.ResourceResizeStrategyAuto,
			}

			result := ShouldEnableResizePolicy(cluster)
			Expect(result).To(BeTrue())
		})
	})

	Describe("GetDefaultResizePolicy", func() {
		It("should return a valid default resize policy", func() {
			policy := GetDefaultResizePolicy()

			Expect(policy).NotTo(BeNil())
			Expect(policy.Strategy).To(Equal(apiv1.ResourceResizeStrategyAuto))
			Expect(policy.CPU).NotTo(BeNil())
			Expect(policy.Memory).NotTo(BeNil())
			Expect(policy.AutoStrategyThresholds).NotTo(BeNil())

			// Check CPU defaults
			Expect(policy.CPU.RestartPolicy).To(Equal(apiv1.ContainerRestartPolicyNotRequired))
			Expect(policy.CPU.MaxIncreasePct).NotTo(BeNil())
			Expect(*policy.CPU.MaxIncreasePct).To(Equal(int32(100)))
			Expect(policy.CPU.MaxDecreasePct).NotTo(BeNil())
			Expect(*policy.CPU.MaxDecreasePct).To(Equal(int32(50)))

			// Check Memory defaults
			Expect(policy.Memory.RestartPolicy).To(Equal(apiv1.ContainerRestartPolicyRestartContainer))
			Expect(policy.Memory.MaxIncreasePct).NotTo(BeNil())
			Expect(*policy.Memory.MaxIncreasePct).To(Equal(int32(50)))
			Expect(policy.Memory.MaxDecreasePct).NotTo(BeNil())
			Expect(*policy.Memory.MaxDecreasePct).To(Equal(int32(25)))

			// Check thresholds
			Expect(policy.AutoStrategyThresholds.CPUIncreaseThreshold).To(Equal(int32(100)))
			Expect(policy.AutoStrategyThresholds.MemoryIncreaseThreshold).To(Equal(int32(50)))
			Expect(policy.AutoStrategyThresholds.MemoryDecreaseThreshold).To(Equal(int32(25)))
		})
	})
})
