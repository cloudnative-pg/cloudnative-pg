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

// Package resize contains the logic for in-place pod resizing
package resize

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// DecisionEngine determines the optimal resize strategy
type DecisionEngine struct {
	cluster *apiv1.Cluster
	oldSpec *corev1.ResourceRequirements
	newSpec *corev1.ResourceRequirements
}

// NewDecisionEngine creates a new resize decision engine
func NewDecisionEngine(
	cluster *apiv1.Cluster,
	oldSpec, newSpec *corev1.ResourceRequirements,
) *DecisionEngine {
	return &DecisionEngine{
		cluster: cluster,
		oldSpec: oldSpec,
		newSpec: newSpec,
	}
}

// DetermineStrategy determines the optimal resize strategy
func (r *DecisionEngine) DetermineStrategy() (apiv1.ResourceResizeStrategy, error) {
	policy := r.cluster.Spec.ResourceResizePolicy
	if policy == nil {
		return apiv1.ResourceResizeStrategyRollingUpdate, nil
	}

	switch policy.Strategy {
	case apiv1.ResourceResizeStrategyInPlace:
		if r.canResizeInPlace() {
			return apiv1.ResourceResizeStrategyInPlace, nil
		}
		return apiv1.ResourceResizeStrategyRollingUpdate,
			fmt.Errorf("in-place resize not feasible, falling back to rolling update")

	case apiv1.ResourceResizeStrategyRollingUpdate:
		return apiv1.ResourceResizeStrategyRollingUpdate, nil

	case apiv1.ResourceResizeStrategyAuto:
		return r.selectAutoStrategy(), nil

	default:
		return apiv1.ResourceResizeStrategyRollingUpdate, nil
	}
}

// canResizeInPlace checks if in-place resize is possible
func (r *DecisionEngine) canResizeInPlace() bool {
	// Check if Kubernetes cluster supports in-place resize
	// For Phase 1, we'll assume it's supported and focus on CPU-only changes
	if !r.isInPlaceResizeSupported() {
		return false
	}

	// Check if changes are within configured thresholds
	if !r.isWithinThresholds() {
		return false
	}

	// For Phase 1, only support CPU changes
	return r.isCPUOnlyChange()
}

// selectAutoStrategy selects the best strategy automatically
func (r *DecisionEngine) selectAutoStrategy() apiv1.ResourceResizeStrategy {
	thresholds := r.cluster.Spec.ResourceResizePolicy.AutoStrategyThresholds
	if thresholds == nil {
		thresholds = &apiv1.AutoStrategyThresholds{
			CPUIncreaseThreshold:    100,
			MemoryIncreaseThreshold: 50,
			MemoryDecreaseThreshold: 25,
		}
	}

	cpuChange := r.calculateResourceChange(corev1.ResourceCPU)
	memoryChange := r.calculateResourceChange(corev1.ResourceMemory)

	// Use rolling update for large changes or memory decreases
	if cpuChange > float64(thresholds.CPUIncreaseThreshold) ||
		memoryChange > float64(thresholds.MemoryIncreaseThreshold) ||
		(memoryChange < 0 && -memoryChange > float64(thresholds.MemoryDecreaseThreshold)) {
		return apiv1.ResourceResizeStrategyRollingUpdate
	}

	if r.canResizeInPlace() {
		return apiv1.ResourceResizeStrategyInPlace
	}

	return apiv1.ResourceResizeStrategyRollingUpdate
}

// isInPlaceResizeSupported checks if the Kubernetes cluster supports in-place resize
// For Phase 1, this is a placeholder that returns true
func (r *DecisionEngine) isInPlaceResizeSupported() bool {
	// TODO: In future phases, implement actual Kubernetes version/feature detection
	return true
}

// isWithinThresholds checks if resource changes are within configured thresholds
func (r *DecisionEngine) isWithinThresholds() bool {
	policy := r.cluster.Spec.ResourceResizePolicy
	if policy == nil {
		return false
	}

	// Check CPU thresholds
	if policy.CPU != nil {
		cpuChange := r.calculateResourceChange(corev1.ResourceCPU)
		if cpuChange > 0 && policy.CPU.MaxIncreasePct != nil {
			if cpuChange > float64(*policy.CPU.MaxIncreasePct) {
				return false
			}
		}
		if cpuChange < 0 && policy.CPU.MaxDecreasePct != nil {
			if -cpuChange > float64(*policy.CPU.MaxDecreasePct) {
				return false
			}
		}
	}

	// Check memory thresholds (for completeness, though Phase 1 focuses on CPU)
	if policy.Memory != nil {
		memoryChange := r.calculateResourceChange(corev1.ResourceMemory)
		if memoryChange > 0 && policy.Memory.MaxIncreasePct != nil {
			if memoryChange > float64(*policy.Memory.MaxIncreasePct) {
				return false
			}
		}
		if memoryChange < 0 && policy.Memory.MaxDecreasePct != nil {
			if -memoryChange > float64(*policy.Memory.MaxDecreasePct) {
				return false
			}
		}
	}

	return true
}

// isCPUOnlyChange checks if only CPU resources are changing (Phase 1 limitation)
func (r *DecisionEngine) isCPUOnlyChange() bool {
	oldMemory := r.getResourceQuantity(r.oldSpec, corev1.ResourceMemory)
	newMemory := r.getResourceQuantity(r.newSpec, corev1.ResourceMemory)

	// Memory should not change for Phase 1
	return oldMemory.Equal(newMemory)
}

// calculateResourceChange calculates the percentage change for a resource
func (r *DecisionEngine) calculateResourceChange(resourceName corev1.ResourceName) float64 {
	oldQuantity := r.getResourceQuantity(r.oldSpec, resourceName)
	newQuantity := r.getResourceQuantity(r.newSpec, resourceName)

	if oldQuantity.IsZero() {
		if newQuantity.IsZero() {
			return 0
		}
		// If old is zero but new is not, consider it a 100% increase
		return 100
	}

	oldValue := oldQuantity.AsApproximateFloat64()
	newValue := newQuantity.AsApproximateFloat64()

	return ((newValue - oldValue) / oldValue) * 100
}

// getResourceQuantity safely gets a resource quantity from resource requirements
func (r *DecisionEngine) getResourceQuantity(
	resources *corev1.ResourceRequirements,
	resourceName corev1.ResourceName,
) resource.Quantity {
	if resources == nil {
		return resource.Quantity{}
	}

	// Check limits first, then requests
	if quantity, exists := resources.Limits[resourceName]; exists {
		return quantity
	}
	if quantity, exists := resources.Requests[resourceName]; exists {
		return quantity
	}

	return resource.Quantity{}
}

// GetResizeReason provides a human-readable reason for the resize decision
func (r *DecisionEngine) GetResizeReason(strategy apiv1.ResourceResizeStrategy) string {
	switch strategy {
	case apiv1.ResourceResizeStrategyInPlace:
		return "Resource changes are within thresholds and suitable for in-place resize"
	case apiv1.ResourceResizeStrategyRollingUpdate:
		if !r.isInPlaceResizeSupported() {
			return "Kubernetes cluster does not support in-place pod resizing"
		}
		if !r.isCPUOnlyChange() {
			return "Memory changes not supported in Phase 1, using rolling update"
		}
		if !r.isWithinThresholds() {
			return "Resource changes exceed configured thresholds"
		}
		return "Rolling update selected for safety"
	default:
		return "Unknown resize strategy"
	}
}
