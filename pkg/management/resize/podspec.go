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
)

const (
	// PostgresContainerName is the name of the PostgreSQL container
	PostgresContainerName = "postgres"
)

// UpdatePodSpecForResize configures pod spec with resize policies
func UpdatePodSpecForResize(
	podSpec *corev1.PodSpec,
	cluster *apiv1.Cluster,
) {
	policy := cluster.Spec.ResourceResizePolicy
	if policy == nil {
		return
	}

	// Find PostgreSQL container
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == PostgresContainerName {
			container := &podSpec.Containers[i]

			// Configure resize policies
			if container.ResizePolicy == nil {
				container.ResizePolicy = []corev1.ContainerResizePolicy{}
			}

			// CPU resize policy
			if policy.CPU != nil {
				var restartPolicy corev1.ResourceResizeRestartPolicy
				switch policy.CPU.RestartPolicy {
				case apiv1.ContainerRestartPolicyNotRequired:
					restartPolicy = corev1.NotRequired
				case apiv1.ContainerRestartPolicyRestartContainer:
					restartPolicy = corev1.RestartContainer
				default:
					restartPolicy = corev1.NotRequired
				}

				container.ResizePolicy = append(container.ResizePolicy,
					corev1.ContainerResizePolicy{
						ResourceName:  corev1.ResourceCPU,
						RestartPolicy: restartPolicy,
					})
			}

			// Memory resize policy (for completeness, though Phase 1 focuses on CPU)
			if policy.Memory != nil {
				var restartPolicy corev1.ResourceResizeRestartPolicy
				switch policy.Memory.RestartPolicy {
				case apiv1.ContainerRestartPolicyNotRequired:
					restartPolicy = corev1.NotRequired
				case apiv1.ContainerRestartPolicyRestartContainer:
					restartPolicy = corev1.RestartContainer
				default:
					restartPolicy = corev1.RestartContainer // Default for memory
				}

				container.ResizePolicy = append(container.ResizePolicy,
					corev1.ContainerResizePolicy{
						ResourceName:  corev1.ResourceMemory,
						RestartPolicy: restartPolicy,
					})
			}

			break
		}
	}
}

// ShouldEnableResizePolicy checks if resize policies should be enabled
func ShouldEnableResizePolicy(cluster *apiv1.Cluster) bool {
	return cluster.Spec.ResourceResizePolicy != nil
}

// GetDefaultResizePolicy returns a default resize policy for Auto strategy
func GetDefaultResizePolicy() *apiv1.ResourceResizePolicy {
	return &apiv1.ResourceResizePolicy{
		Strategy: apiv1.ResourceResizeStrategyAuto,
		CPU: &apiv1.ContainerResizePolicy{
			RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
			MaxIncreasePct: int32Ptr(100),
			MaxDecreasePct: int32Ptr(50),
		},
		Memory: &apiv1.ContainerResizePolicy{
			RestartPolicy:  apiv1.ContainerRestartPolicyRestartContainer,
			MaxIncreasePct: int32Ptr(50),
			MaxDecreasePct: int32Ptr(25),
		},
		AutoStrategyThresholds: &apiv1.AutoStrategyThresholds{
			CPUIncreaseThreshold:    100,
			MemoryIncreaseThreshold: 50,
			MemoryDecreaseThreshold: 25,
		},
	}
}

// int32Ptr returns a pointer to an int32 value
func int32Ptr(i int32) *int32 {
	return &i
}
