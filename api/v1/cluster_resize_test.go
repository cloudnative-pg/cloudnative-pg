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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster resize API types", Label("resizing"), func() {
	var cluster *Cluster

	BeforeEach(func() {
		cluster = &Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: ClusterSpec{
				Instances: 3,
			},
		}
	})

	Describe("ResourceResizePolicy", func() {
		It("should allow setting all resize policy fields", func() {
			maxInc := int32(100)
			maxDec := int32(50)

			cluster.Spec.ResourceResizePolicy = &ResourceResizePolicy{
				Strategy: ResourceResizeStrategyAuto,
				CPU: &ContainerResizePolicy{
					RestartPolicy:  ContainerRestartPolicyNotRequired,
					MaxIncreasePct: &maxInc,
					MaxDecreasePct: &maxDec,
				},
				Memory: &ContainerResizePolicy{
					RestartPolicy:  ContainerRestartPolicyRestartContainer,
					MaxIncreasePct: &maxInc,
					MaxDecreasePct: &maxDec,
				},
				AutoStrategyThresholds: &AutoStrategyThresholds{
					CPUIncreaseThreshold:    100,
					MemoryIncreaseThreshold: 50,
					MemoryDecreaseThreshold: 25,
				},
			}

			Expect(cluster.Spec.ResourceResizePolicy).NotTo(BeNil())
			Expect(cluster.Spec.ResourceResizePolicy.Strategy).To(Equal(ResourceResizeStrategyAuto))
			Expect(cluster.Spec.ResourceResizePolicy.CPU).NotTo(BeNil())
			Expect(cluster.Spec.ResourceResizePolicy.Memory).NotTo(BeNil())
			Expect(cluster.Spec.ResourceResizePolicy.AutoStrategyThresholds).NotTo(BeNil())
		})

		It("should handle minimal configuration", func() {
			cluster.Spec.ResourceResizePolicy = &ResourceResizePolicy{
				Strategy: ResourceResizeStrategyInPlace,
			}

			Expect(cluster.Spec.ResourceResizePolicy.Strategy).To(Equal(ResourceResizeStrategyInPlace))
			Expect(cluster.Spec.ResourceResizePolicy.CPU).To(BeNil())
			Expect(cluster.Spec.ResourceResizePolicy.Memory).To(BeNil())
			Expect(cluster.Spec.ResourceResizePolicy.AutoStrategyThresholds).To(BeNil())
		})
	})

	Describe("ResizeStatus", func() {
		It("should allow setting all resize status fields", func() {
			now := metav1.Now()
			cluster.Status.ResizeStatus = &ResizeStatus{
				Strategy:    ResourceResizeStrategyInPlace,
				Phase:       ResizePhaseInProgress,
				StartedAt:   &now,
				CompletedAt: nil,
				InstancesStatus: []InstanceResizeStatus{
					{
						Name:     "test-cluster-1",
						Phase:    ResizePhaseCompleted,
						Strategy: ResourceResizeStrategyInPlace,
						Message:  "Resize completed successfully",
					},
					{
						Name:     "test-cluster-2",
						Phase:    ResizePhaseInProgress,
						Strategy: ResourceResizeStrategyInPlace,
						Message:  "Resize in progress",
					},
				},
				Message: "Resizing cluster instances",
			}

			Expect(cluster.Status.ResizeStatus).NotTo(BeNil())
			Expect(cluster.Status.ResizeStatus.Strategy).To(Equal(ResourceResizeStrategyInPlace))
			Expect(cluster.Status.ResizeStatus.Phase).To(Equal(ResizePhaseInProgress))
			Expect(cluster.Status.ResizeStatus.StartedAt).NotTo(BeNil())
			Expect(cluster.Status.ResizeStatus.CompletedAt).To(BeNil())
			Expect(cluster.Status.ResizeStatus.InstancesStatus).To(HaveLen(2))
			Expect(cluster.Status.ResizeStatus.Message).To(Equal("Resizing cluster instances"))
		})

		It("should handle completed resize status", func() {
			now := metav1.Now()
			completed := metav1.Now()

			cluster.Status.ResizeStatus = &ResizeStatus{
				Strategy:    ResourceResizeStrategyInPlace,
				Phase:       ResizePhaseCompleted,
				StartedAt:   &now,
				CompletedAt: &completed,
				Message:     "Resize completed successfully",
			}

			Expect(cluster.Status.ResizeStatus.Phase).To(Equal(ResizePhaseCompleted))
			Expect(cluster.Status.ResizeStatus.CompletedAt).NotTo(BeNil())
		})

		It("should handle failed resize status", func() {
			now := metav1.Now()

			cluster.Status.ResizeStatus = &ResizeStatus{
				Strategy:  ResourceResizeStrategyInPlace,
				Phase:     ResizePhaseFailed,
				StartedAt: &now,
				Message:   "Resize failed due to insufficient resources",
			}

			Expect(cluster.Status.ResizeStatus.Phase).To(Equal(ResizePhaseFailed))
			Expect(cluster.Status.ResizeStatus.Message).To(ContainSubstring("failed"))
		})
	})

	Describe("ResourceResizeStrategy constants", func() {
		It("should have correct string values", func() {
			Expect(string(ResourceResizeStrategyInPlace)).To(Equal("InPlace"))
			Expect(string(ResourceResizeStrategyRollingUpdate)).To(Equal("RollingUpdate"))
			Expect(string(ResourceResizeStrategyAuto)).To(Equal("Auto"))
		})
	})

	Describe("ResizePhase constants", func() {
		It("should have correct string values", func() {
			Expect(string(ResizePhaseInProgress)).To(Equal("InProgress"))
			Expect(string(ResizePhasePending)).To(Equal("Pending"))
			Expect(string(ResizePhaseCompleted)).To(Equal("Completed"))
			Expect(string(ResizePhaseFailed)).To(Equal("Failed"))
		})
	})

	Describe("ContainerRestartPolicy constants", func() {
		It("should have correct string values", func() {
			Expect(string(ContainerRestartPolicyNotRequired)).To(Equal("NotRequired"))
			Expect(string(ContainerRestartPolicyRestartContainer)).To(Equal("RestartContainer"))
		})
	})

	Describe("ContainerResizePolicy validation", func() {
		It("should accept valid percentage values", func() {
			maxInc := int32(100)
			maxDec := int32(50)

			policy := &ContainerResizePolicy{
				RestartPolicy:  ContainerRestartPolicyNotRequired,
				MaxIncreasePct: &maxInc,
				MaxDecreasePct: &maxDec,
			}

			Expect(policy.MaxIncreasePct).NotTo(BeNil())
			Expect(*policy.MaxIncreasePct).To(Equal(int32(100)))
			Expect(policy.MaxDecreasePct).NotTo(BeNil())
			Expect(*policy.MaxDecreasePct).To(Equal(int32(50)))
		})

		It("should handle nil percentage values", func() {
			policy := &ContainerResizePolicy{
				RestartPolicy: ContainerRestartPolicyRestartContainer,
			}

			Expect(policy.MaxIncreasePct).To(BeNil())
			Expect(policy.MaxDecreasePct).To(BeNil())
		})
	})

	Describe("AutoStrategyThresholds", func() {
		It("should allow setting threshold values", func() {
			thresholds := &AutoStrategyThresholds{
				CPUIncreaseThreshold:    75,
				MemoryIncreaseThreshold: 40,
				MemoryDecreaseThreshold: 20,
			}

			Expect(thresholds.CPUIncreaseThreshold).To(Equal(int32(75)))
			Expect(thresholds.MemoryIncreaseThreshold).To(Equal(int32(40)))
			Expect(thresholds.MemoryDecreaseThreshold).To(Equal(int32(20)))
		})

		It("should work with zero values", func() {
			thresholds := &AutoStrategyThresholds{
				CPUIncreaseThreshold:    0,
				MemoryIncreaseThreshold: 0,
				MemoryDecreaseThreshold: 0,
			}

			Expect(thresholds.CPUIncreaseThreshold).To(Equal(int32(0)))
			Expect(thresholds.MemoryIncreaseThreshold).To(Equal(int32(0)))
			Expect(thresholds.MemoryDecreaseThreshold).To(Equal(int32(0)))
		})
	})

	Describe("InstanceResizeStatus", func() {
		It("should track individual instance resize status", func() {
			instanceStatus := InstanceResizeStatus{
				Name:     "test-cluster-1",
				Phase:    ResizePhaseCompleted,
				Strategy: ResourceResizeStrategyInPlace,
				Message:  "CPU resize completed successfully",
			}

			Expect(instanceStatus.Name).To(Equal("test-cluster-1"))
			Expect(instanceStatus.Phase).To(Equal(ResizePhaseCompleted))
			Expect(instanceStatus.Strategy).To(Equal(ResourceResizeStrategyInPlace))
			Expect(instanceStatus.Message).To(ContainSubstring("CPU resize"))
		})
	})
})
