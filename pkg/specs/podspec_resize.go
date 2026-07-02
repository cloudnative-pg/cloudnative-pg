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
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ContainerResourceDrift describes a container whose resources differ
// between the current and the target pod spec
type ContainerResourceDrift struct {
	Name          string
	InitContainer bool
	Target        corev1.ResourceRequirements
}

// ComparePodSpecsIgnoringContainerResources compares two pod specs like
// ComparePodSpecs, but ignoring any difference in the resources of their
// containers and init containers
func ComparePodSpecsIgnoringContainerResources(
	currentPodSpec, targetPodSpec corev1.PodSpec,
) (bool, string) {
	stripResources := func(spec corev1.PodSpec) corev1.PodSpec {
		stripped := *spec.DeepCopy()
		for i := range stripped.Containers {
			stripped.Containers[i].Resources = corev1.ResourceRequirements{}
		}
		for i := range stripped.InitContainers {
			stripped.InitContainers[i].Resources = corev1.ResourceRequirements{}
		}
		return stripped
	}

	return ComparePodSpecs(stripResources(currentPodSpec), stripResources(targetPodSpec))
}

// GetContainerResourceDrifts returns the containers of the current pod spec
// whose resources differ from the same-named container of the target pod
// spec. Containers present on one side only are ignored: that is a spec
// drift, not a resource drift, and is detected by the pod spec comparison.
func GetContainerResourceDrifts(current, target *corev1.PodSpec) []ContainerResourceDrift {
	var drifts []ContainerResourceDrift

	collect := func(currentContainers, targetContainers []corev1.Container, initContainer bool) {
		targetByName := make(map[string]*corev1.Container, len(targetContainers))
		for i := range targetContainers {
			targetByName[targetContainers[i].Name] = &targetContainers[i]
		}
		for i := range currentContainers {
			targetContainer, found := targetByName[currentContainers[i].Name]
			if !found {
				continue
			}
			if resourceListsEqual(currentContainers[i].Resources.Requests, targetContainer.Resources.Requests) &&
				resourceListsEqual(currentContainers[i].Resources.Limits, targetContainer.Resources.Limits) {
				continue
			}
			drifts = append(drifts, ContainerResourceDrift{
				Name:          currentContainers[i].Name,
				InitContainer: initContainer,
				Target:        targetContainer.Resources,
			})
		}
	}

	collect(current.Containers, target.Containers, false)
	collect(current.InitContainers, target.InitContainers, true)

	return drifts
}

// GetResizableContainerResourceDrifts returns the resource drifts of the
// containers that Kubernetes can resize in place: regular containers and
// native sidecars. Run-once init containers are excluded: they have already
// terminated, cannot be resized, and their resources only matter when the
// pod is created, so their drift is left to the next pod recreation.
func GetResizableContainerResourceDrifts(current, target *corev1.PodSpec) []ContainerResourceDrift {
	drifts := GetContainerResourceDrifts(current, target)
	resizable := make([]ContainerResourceDrift, 0, len(drifts))
	for _, drift := range drifts {
		if drift.InitContainer && !isNativeSidecar(current.InitContainers, drift.Name) {
			continue
		}
		resizable = append(resizable, drift)
	}
	return resizable
}

// CanResizeInPlace verifies that the resource drifts between the current and
// the target pod spec can be applied through the resize subresource without
// disrupting the pod: only cpu and memory values may change, no entry may be
// added or removed, and memory limits may not be decreased (PostgreSQL never
// releases its shared memory, so the kubelet would keep the resize pending
// forever). Run-once init container drifts are ignored, being deferred to
// the next pod recreation. The QoS class invariant is not checked here: the
// API server enforces it and a rejected patch makes the operator fall back
// to recreating the pod.
func CanResizeInPlace(current, target *corev1.PodSpec) (bool, string) {
	for _, drift := range GetResizableContainerResourceDrifts(current, target) {
		currentResources := getContainerResources(current, drift.Name, drift.InitContainer)
		if ok, reason := canResizeResources(drift.Name, currentResources, drift.Target); !ok {
			return false, reason
		}
	}

	return true, ""
}

func canResizeResources(containerName string, current, target corev1.ResourceRequirements) (bool, string) {
	if ok, reason := canResizeResourceList(containerName, "requests", current.Requests, target.Requests); !ok {
		return false, reason
	}
	if ok, reason := canResizeResourceList(containerName, "limits", current.Limits, target.Limits); !ok {
		return false, reason
	}

	currentMemoryLimit, hasCurrentMemoryLimit := current.Limits[corev1.ResourceMemory]
	targetMemoryLimit, hasTargetMemoryLimit := target.Limits[corev1.ResourceMemory]
	if hasCurrentMemoryLimit && hasTargetMemoryLimit && targetMemoryLimit.Cmp(currentMemoryLimit) < 0 {
		return false, fmt.Sprintf(
			"container %s: the memory limit cannot be decreased in place", containerName)
	}

	return true, ""
}

func canResizeResourceList(containerName, listName string, current, target corev1.ResourceList) (bool, string) {
	for name := range current {
		if _, found := target[name]; !found {
			return false, fmt.Sprintf(
				"container %s: the %s entry for %s cannot be removed in place", containerName, listName, name)
		}
	}
	for name, targetValue := range target {
		currentValue, found := current[name]
		if !found {
			return false, fmt.Sprintf(
				"container %s: the %s entry for %s cannot be added in place", containerName, listName, name)
		}
		if currentValue.Cmp(targetValue) == 0 {
			continue
		}
		if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
			return false, fmt.Sprintf(
				"container %s: %s cannot be resized in place", containerName, name)
		}
	}

	return true, ""
}

// resourceListsEqual compares two resource lists semantically, treating nil
// and empty lists as equal
func resourceListsEqual(current, target corev1.ResourceList) bool {
	if len(current) != len(target) {
		return false
	}
	for name, currentValue := range current {
		targetValue, found := target[name]
		if !found || currentValue.Cmp(targetValue) != 0 {
			return false
		}
	}
	return true
}

func isNativeSidecar(initContainers []corev1.Container, name string) bool {
	for i := range initContainers {
		if initContainers[i].Name != name {
			continue
		}
		return initContainers[i].RestartPolicy != nil &&
			*initContainers[i].RestartPolicy == corev1.ContainerRestartPolicyAlways
	}
	return false
}

func getContainerResources(spec *corev1.PodSpec, name string, initContainer bool) corev1.ResourceRequirements {
	containers := spec.Containers
	if initContainer {
		containers = spec.InitContainers
	}
	for i := range containers {
		if containers[i].Name == name {
			return containers[i].Resources
		}
	}
	return corev1.ResourceRequirements{}
}
