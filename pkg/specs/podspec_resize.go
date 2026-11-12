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
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// ResizeInPlace applies the container resources of the target pod spec to a
// running pod. The stored pod spec, as recorded by the operator when the pod
// was created, is compared with the target to find what changed, and gets
// the target resources. The live pod spec gets the values to send to the
// resize subresource of the pod: the entries it got at admission time, for
// example from a LimitRange, are preserved.
//
// It returns false when the change cannot be applied without recreating the
// pod, and in that case the content of the stored and live specs is not
// meaningful: only the cpu and memory values of existing entries may change,
// memory limits may not go below the ones the kubelet enforces, claims may
// not change and the resize policy of the container may not restart it
// for a resource that changes.
// Run-once init containers have already terminated and cannot be resized:
// their resources are only recorded in the stored spec, and apply when the
// pod is recreated.
//
// The status of the pod tells which limits the kubelet enforces.
func ResizeInPlace(stored, live, target *corev1.PodSpec, status *corev1.PodStatus) bool {
	return resizeContainers(stored.Containers, live.Containers, target.Containers, status, false) &&
		resizeContainers(stored.InitContainers, live.InitContainers, target.InitContainers, status, true)
}

func resizeContainers(
	stored, live, target []corev1.Container,
	status *corev1.PodStatus,
	initContainers bool,
) bool {
	for i := range stored {
		hasSameName := func(container corev1.Container) bool { return container.Name == stored[i].Name }

		targetIdx := slices.IndexFunc(target, hasSameName)
		if targetIdx < 0 {
			// Not a resource change: it is detected by the pod spec comparison
			continue
		}

		current := defaultRequestsFromLimits(stored[i].Resources)
		desired := defaultRequestsFromLimits(target[targetIdx].Resources)
		claimsChanged := !equality.Semantic.DeepEqual(
			stored[i].Resources.Claims, target[targetIdx].Resources.Claims)
		stored[i].Resources = *target[targetIdx].Resources.DeepCopy()

		requests, requestsResizable := changedEntries(current.Requests, desired.Requests)
		limits, limitsResizable := changedEntries(current.Limits, desired.Limits)

		// The resources the operator did not change are left alone, even
		// when the live ones were changed by other actors
		if !claimsChanged && requestsResizable && limitsResizable && len(requests)+len(limits) == 0 {
			continue
		}

		if initContainers && (stored[i].RestartPolicy == nil ||
			*stored[i].RestartPolicy != corev1.ContainerRestartPolicyAlways) {
			continue
		}

		liveIdx := slices.IndexFunc(live, hasSameName)
		if claimsChanged || !requestsResizable || !limitsResizable || liveIdx < 0 ||
			isGuaranteed(current) != isGuaranteed(desired) ||
			!canResizeMemoryLimit(limits, enforcedLimits(&live[liveIdx], status)) ||
			restartsOnResize(live[liveIdx].ResizePolicy, requests, limits) {
			return false
		}
		live[liveIdx].Resources.Requests = overlay(live[liveIdx].Resources.Requests, requests)
		live[liveIdx].Resources.Limits = overlay(live[liveIdx].Resources.Limits, limits)
	}

	return true
}

// enforcedLimits returns the limits of the container the kubelet enforces.
// While a resize is pending, the live spec holds the wanted limits and the
// status the enforced ones.
func enforcedLimits(live *corev1.Container, status *corev1.PodStatus) corev1.ResourceList {
	for _, containerStatus := range slices.Concat(status.ContainerStatuses, status.InitContainerStatuses) {
		if containerStatus.Name == live.Name && containerStatus.Resources != nil {
			return containerStatus.Resources.Limits
		}
	}
	return live.Resources.Limits
}

// canResizeMemoryLimit tells whether the changed limits can be applied
// without lowering the memory limit below the enforced one.
//
// A memory limit decrease is never applied in place. The kubelet lowers
// the cgroup limit only when the current usage of the container is
// already below the new value, and PostgreSQL keeps its shared memory
// (shared_buffers above all) allocated for the whole life of the
// postmaster, so on a warmed-up instance the resize would stay pending
// indefinitely without reporting any error. Should the usage momentarily
// dip below the target and the decrease land, the instance would then
// run with a memory configuration sized for the old limit inside a
// smaller cgroup, one allocation spike away from an OOM kill. A
// recreated pod starts under the new limit from the beginning, making
// the outcome deterministic.
//
// The comparison is against the enforced limit, not the recorded one: a
// deferred increase followed by its revert goes back to the enforced
// value, which needs no decrease. Without an enforced limit to compare
// with, any change of the memory limit is refused.
func canResizeMemoryLimit(changedLimits, enforced corev1.ResourceList) bool {
	memoryLimit, changed := changedLimits[corev1.ResourceMemory]
	if !changed {
		return true
	}
	enforcedMemoryLimit, isEnforced := enforced[corev1.ResourceMemory]
	return isEnforced && memoryLimit.Cmp(enforcedMemoryLimit) >= 0
}

// restartsOnResize tells whether the resize policy restarts the container
// for a resource that changes
func restartsOnResize(policies []corev1.ContainerResizePolicy, requests, limits corev1.ResourceList) bool {
	return slices.ContainsFunc(policies, func(policy corev1.ContainerResizePolicy) bool {
		_, requestChanged := requests[policy.ResourceName]
		_, limitChanged := limits[policy.ResourceName]
		return policy.RestartPolicy == corev1.RestartContainer && (requestChanged || limitChanged)
	})
}

// isGuaranteed tells whether the resources give the container the Guaranteed
// QoS class. The API server rejects a resize that changes it.
func isGuaranteed(resources corev1.ResourceRequirements) bool {
	return !resources.Limits.Cpu().IsZero() && !resources.Limits.Memory().IsZero() &&
		resources.Requests.Cpu().Cmp(*resources.Limits.Cpu()) == 0 &&
		resources.Requests.Memory().Cmp(*resources.Limits.Memory()) == 0
}

// changedEntries returns the entries whose value differs between from and
// to. It reports false when an entry is added or removed, or when a resource
// other than cpu and memory changes.
func changedEntries(from, to corev1.ResourceList) (corev1.ResourceList, bool) {
	changed := corev1.ResourceList{}
	for name, quantity := range to {
		previous, found := from[name]
		if !found {
			return nil, false
		}
		if previous.Cmp(quantity) == 0 {
			continue
		}
		if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
			return nil, false
		}
		changed[name] = quantity
	}
	return changed, len(from) == len(to)
}

// defaultRequestsFromLimits sets the missing requests to the corresponding
// limits, as the API server does when a pod is created
func defaultRequestsFromLimits(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: overlay(resources.Limits, resources.Requests),
		Limits:   resources.Limits,
	}
}

// overlay returns a new resource list with the given entries applied over
// the base one
func overlay(base, entries corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	maps.Copy(result, base)
	maps.Copy(result, entries)
	return result
}
