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
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ResizePod returns the pod with the container resources that changed
// between the stored and the target pod spec applied, and recorded in its
// pod spec annotation. Only the changed entries are applied: the others may
// belong to other actors (LimitRange, webhooks). It returns a nil pod when
// the change needs a new pod, and the reason when a resource rule says so.
func ResizePod(pod *corev1.Pod, stored, target *corev1.PodSpec) (*corev1.Pod, string) {
	resized, recorded := pod.DeepCopy(), stored.DeepCopy()
	for _, containers := range []struct {
		live, stored, target []corev1.Container
		init                 bool
	}{
		{resized.Spec.Containers, recorded.Containers, target.Containers, false},
		{resized.Spec.InitContainers, recorded.InitContainers, target.InitContainers, true},
	} {
		for i := range containers.stored {
			from := containers.stored[i].Resources
			hasName := func(container corev1.Container) bool { return container.Name == containers.stored[i].Name }
			targetIdx := slices.IndexFunc(containers.target, hasName)
			if targetIdx < 0 || equality.Semantic.DeepEqual(from, containers.target[targetIdx].Resources) {
				continue
			}
			containers.stored[i].Resources = containers.target[targetIdx].Resources

			liveIdx := slices.IndexFunc(containers.live, hasName)
			if liveIdx < 0 {
				return nil, ""
			}
			live := &containers.live[liveIdx]
			if containers.init && (live.RestartPolicy == nil || *live.RestartPolicy != corev1.ContainerRestartPolicyAlways) {
				// A run-once init container has already terminated
				continue
			}

			// While a resize is pending, the spec holds the wanted limits
			// and the status the ones the kubelet enforces
			enforced := live.Resources.Limits
			for _, status := range slices.Concat(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses) {
				if status.Name == live.Name && status.Resources != nil {
					enforced = status.Resources.Limits
				}
			}
			if blocker := resizeContainer(live, enforced, from, containers.target[targetIdx].Resources); blocker != "" {
				return nil, fmt.Sprintf("container %s: %s", live.Name, blocker)
			}
		}
	}

	// With the resources recorded, anything else that differs needs a new pod
	if match, _ := ComparePodSpecs(*recorded, *target); !match {
		return nil, ""
	}
	// A pod spec always marshals
	podSpecMarshaled, _ := json.Marshal(recorded)
	resized.Annotations[utils.PodSpecAnnotationName] = string(podSpecMarshaled)

	return resized, ""
}

// resizeContainer applies to the live container the resources that changed
// between from and to. It returns the reason why the container must be
// recreated instead, if any.
func resizeContainer(
	live *corev1.Container,
	enforcedLimits corev1.ResourceList,
	from, to corev1.ResourceRequirements,
) string {
	// The API server sets the missing requests to their limits
	from.Requests, to.Requests = overlay(from.Limits, from.Requests), overlay(to.Limits, to.Requests)

	requests, requestsResizable := changedEntries(from.Requests, to.Requests)
	limits, limitsResizable := changedEntries(from.Limits, to.Limits)
	if !requestsResizable || !limitsResizable || !equality.Semantic.DeepEqual(from.Claims, to.Claims) {
		return "only the cpu and memory values can change in place"
	}

	// PostgreSQL keeps its shared memory, and the kubelet never applies a
	// memory limit below the current usage: the resize would stay stuck.
	// Without an enforced limit to compare with, any change is refused.
	memoryLimit, changed := limits[corev1.ResourceMemory]
	enforcedMemoryLimit, enforced := enforcedLimits[corev1.ResourceMemory]
	if changed && (!enforced || memoryLimit.Cmp(enforcedMemoryLimit) < 0) {
		return "the memory limit cannot be decreased in place"
	}

	// The API server rejects a resize that changes the QoS class
	guaranteed := func(resources corev1.ResourceRequirements) bool {
		return !resources.Limits.Cpu().IsZero() && !resources.Limits.Memory().IsZero() &&
			resources.Requests.Cpu().Cmp(*resources.Limits.Cpu()) == 0 &&
			resources.Requests.Memory().Cmp(*resources.Limits.Memory()) == 0
	}
	if guaranteed(from) != guaranteed(to) {
		return "the QoS class of the pod would change"
	}

	// The kubelet restarts the container when a resource it changes has
	// the RestartContainer policy
	if slices.ContainsFunc(live.ResizePolicy, func(policy corev1.ContainerResizePolicy) bool {
		_, requestChanged := requests[policy.ResourceName]
		_, limitChanged := limits[policy.ResourceName]
		return policy.RestartPolicy == corev1.RestartContainer && (requestChanged || limitChanged)
	}) {
		return "its resize policy restarts the container"
	}

	live.Resources.Requests = overlay(live.Resources.Requests, requests)
	live.Resources.Limits = overlay(live.Resources.Limits, limits)
	return ""
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

// overlay returns a new resource list with the given entries applied over
// the base one
func overlay(base, entries corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	maps.Copy(result, base)
	maps.Copy(result, entries)
	return result
}
