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

// Package main demonstrates the basic in-place pod resizing functionality
package main

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/resize"
)

func main() {
	fmt.Println("CloudNativePG In-Place Pod Resizing Demo (Phase 1)")
	fmt.Println("==================================================")

	// Create a sample cluster with resize policy
	cluster := &apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			Instances: 3,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1000m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2000m"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			ResourceResizePolicy: &apiv1.ResourceResizePolicy{
				Strategy: apiv1.ResourceResizeStrategyInPlace,
				CPU: &apiv1.ContainerResizePolicy{
					RestartPolicy:  apiv1.ContainerRestartPolicyNotRequired,
					MaxIncreasePct: ptr.To(int32(100)),
					MaxDecreasePct: ptr.To(int32(50)),
				},
				Memory: &apiv1.ContainerResizePolicy{
					RestartPolicy:  apiv1.ContainerRestartPolicyRestartContainer,
					MaxIncreasePct: ptr.To(int32(50)),
					MaxDecreasePct: ptr.To(int32(25)),
				},
			},
		},
	}

	fmt.Printf("Initial cluster configuration:\n")
	cpuReq := cluster.Spec.Resources.Requests[corev1.ResourceCPU]
	cpuLim := cluster.Spec.Resources.Limits[corev1.ResourceCPU]
	memReq := cluster.Spec.Resources.Requests[corev1.ResourceMemory]
	memLim := cluster.Spec.Resources.Limits[corev1.ResourceMemory]
	fmt.Printf("  CPU: %s -> %s\n", cpuReq.String(), cpuLim.String())
	fmt.Printf("  Memory: %s -> %s\n", memReq.String(), memLim.String())
	fmt.Printf("  Strategy: %s\n\n", cluster.Spec.ResourceResizePolicy.Strategy)

	// Scenario 1: CPU-only increase within thresholds
	fmt.Println("Scenario 1: CPU increase within thresholds (50%)")
	fmt.Println("------------------------------------------------")

	oldSpec := &cluster.Spec.Resources
	newSpec := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1500m"), // 50% increase
			corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("3000m"), // 50% increase
			corev1.ResourceMemory: resource.MustParse("4Gi"),   // No change
		},
	}

	engine := resize.NewDecisionEngine(cluster, oldSpec, newSpec)
	strategy, err := engine.DetermineStrategy()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Selected strategy: %s\n", strategy)
	}

	// Scenario 2: CPU increase exceeding thresholds
	fmt.Println("Scenario 2: CPU increase exceeding thresholds (150%)")
	fmt.Println("----------------------------------------------------")

	newSpec2 := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2500m"), // 150% increase
			corev1.ResourceMemory: resource.MustParse("2Gi"),   // No change
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("5000m"), // 150% increase
			corev1.ResourceMemory: resource.MustParse("4Gi"),   // No change
		},
	}

	engine2 := resize.NewDecisionEngine(cluster, oldSpec, newSpec2)
	strategy2, err2 := engine2.DetermineStrategy()
	if err2 != nil {
		fmt.Printf("Error: %v\n", err2)
	} else {
		fmt.Printf("Selected strategy: %s\n", strategy2)
	}

	// Scenario 3: Memory change (should trigger rolling update in Phase 1)
	fmt.Println("Scenario 3: Memory change (Phase 1 limitation)")
	fmt.Println("----------------------------------------------")

	newSpec3 := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"), // No change
			corev1.ResourceMemory: resource.MustParse("3Gi"),   // 50% increase
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"), // No change
			corev1.ResourceMemory: resource.MustParse("6Gi"),   // 50% increase
		},
	}

	engine3 := resize.NewDecisionEngine(cluster, oldSpec, newSpec3)
	strategy3, err3 := engine3.DetermineStrategy()
	if err3 != nil {
		fmt.Printf("Error: %v\n", err3)
	} else {
		fmt.Printf("Selected strategy: %s\n", strategy3)
	}

	// Demonstrate pod spec configuration
	fmt.Println("Pod Spec Configuration Demo")
	fmt.Println("---------------------------")

	podSpec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "postgres",
				Image: "postgres:15",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2000m"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
		},
	}

	fmt.Printf("Before resize policy configuration:\n")
	fmt.Printf("  PostgreSQL container resize policies: %d\n",
		len(podSpec.Containers[0].ResizePolicy))

	resize.UpdatePodSpecForResize(podSpec, cluster)

	fmt.Printf("After resize policy configuration:\n")
	fmt.Printf("  PostgreSQL container resize policies: %d\n",
		len(podSpec.Containers[0].ResizePolicy))

	for _, policy := range podSpec.Containers[0].ResizePolicy {
		fmt.Printf("    - Resource: %s, Restart Policy: %s\n",
			policy.ResourceName, policy.RestartPolicy)
	}

	fmt.Println("\nDemo completed! This shows Phase 1 implementation focusing on CPU-only changes.")
}
