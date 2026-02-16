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
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// ComparePodSpecs compares two pod specs, returns true iff they are equivalent, and
// if they are not, points out the first discrepancy.
func ComparePodSpecs(
	currentPodSpec, targetPodSpec corev1.PodSpec,
) (bool, string) {
	comparisons := map[string]func() (bool, string){
		"volumes": func() (bool, string) {
			return compareVolumes(currentPodSpec.Volumes, targetPodSpec.Volumes)
		},
		"containers": func() (bool, string) {
			return compareContainers(currentPodSpec.Containers, targetPodSpec.Containers)
		},
		"init-containers": func() (bool, string) {
			extractContainersForComparison := func(passedContainers []corev1.Container) []corev1.Container {
				var containers []corev1.Container
				for _, container := range passedContainers {
					if container.Name == BootstrapControllerContainerName {
						// ignore the bootstrap controller init container. We handle it inside checkPodSpecIsOutdated.
						continue
					}
					containers = append(containers, container)
				}
				return containers
			}
			return compareContainers(
				extractContainersForComparison(currentPodSpec.InitContainers),
				extractContainersForComparison(targetPodSpec.InitContainers),
			)
		},
	}

	for comp, f := range comparisons {
		areEqual, diff := f()
		if areEqual {
			continue
		}
		return false, fmt.Sprintf("%s: %s", comp, diff)
	}

	genericComparisons := map[string]func() bool{
		"security-context": func() bool {
			return reflect.DeepEqual(currentPodSpec.SecurityContext, targetPodSpec.SecurityContext)
		},
		"affinity": func() bool {
			return reflect.DeepEqual(currentPodSpec.Affinity, targetPodSpec.Affinity)
		},
		"tolerations": func() bool {
			return reflect.DeepEqual(currentPodSpec.Tolerations, targetPodSpec.Tolerations)
		},
		"node-selector": func() bool {
			return reflect.DeepEqual(currentPodSpec.NodeSelector, targetPodSpec.NodeSelector)
		},
		"topology-spread-constraints": func() bool {
			return reflect.DeepEqual(currentPodSpec.TopologySpreadConstraints, targetPodSpec.TopologySpreadConstraints)
		},
		"service-account-name": func() bool {
			return currentPodSpec.ServiceAccountName == targetPodSpec.ServiceAccountName
		},
		"scheduler-name": func() bool {
			return currentPodSpec.SchedulerName == targetPodSpec.SchedulerName
		},
		"hostname": func() bool {
			return currentPodSpec.Hostname == targetPodSpec.Hostname
		},
		"termination-grace-period": func() bool {
			return (currentPodSpec.TerminationGracePeriodSeconds == nil && targetPodSpec.TerminationGracePeriodSeconds == nil) ||
				(currentPodSpec.TerminationGracePeriodSeconds != nil && targetPodSpec.TerminationGracePeriodSeconds != nil &&
					*currentPodSpec.TerminationGracePeriodSeconds == *targetPodSpec.TerminationGracePeriodSeconds)
		},
	}

	for comp, f := range genericComparisons {
		areEqual := f()
		if areEqual {
			continue
		}
		return false, comp
	}

	return true, ""
}

// compareMaps returns true iff the maps are equivalent, otherwise returns
// false, and the first difference found
func compareMaps[V comparable](current, target map[string]V) (bool, string) {
	for name, currentValue := range current {
		targetValue, found := target[name]
		if !found {
			return false, fmt.Sprintf("element %s has been removed", name)
		}
		deepEqual := reflect.DeepEqual(currentValue, targetValue)
		if !deepEqual {
			return false, fmt.Sprintf("element %s has differing value", name)
		}
	}
	for name := range target {
		_, found := current[name]
		if !found {
			return false, fmt.Sprintf("element %s has been added", name)
		}
		// if the key is in both maps, the values have been compared in the previous loop
	}
	return true, ""
}

// shouldIgnoreCurrentVolume checks if a volume or mount is the superuser or app
// mount, which had been added, superflously, in previous versions. If so, ignores
// it for the PodSpec drift detector, to avoid unnecessary restarts
//
// TODO: delete this function after minor version 1.24 is discontinued
func shouldIgnoreCurrentVolume(name string) bool {
	return name == "superuser-secret" || name == "app-secret"
}

// normalizeVolumeName maps old unprefixed volume names to the new prefixed
// scheme (ext- for extensions, tbs- for tablespaces) to avoid spurious
// pod restarts on upgrade.
//
// NOTE: extensions named "ext_*" or tablespaces named "tbs_*" will have
// old sanitized names that already start with "ext-" or "tbs-", causing
// this function to skip normalization. This results in one spurious pod
// restart for the affected cluster on the first reconciliation after upgrade.
//
// TODO: delete this function after minor version 1.28 is discontinued
func normalizeVolumeName(vol corev1.Volume) string {
	name := vol.Name

	if vol.Image != nil && !strings.HasPrefix(name, "ext-") {
		return SanitizeExtensionNameForVolume(name)
	}

	if vol.PersistentVolumeClaim != nil &&
		strings.Contains(vol.PersistentVolumeClaim.ClaimName, apiv1.TablespaceVolumeInfix) &&
		!strings.HasPrefix(name, "tbs-") {
		return "tbs-" + name
	}

	return name
}

// normalizeVolumeMountName maps old unprefixed volume mount names to the new
// prefixed scheme based on mount path. See normalizeVolumeName for known
// limitations with "ext_*" and "tbs_*" naming patterns.
//
// TODO: delete this function after minor version 1.28 is discontinued
func normalizeVolumeMountName(mount corev1.VolumeMount) string {
	name := mount.Name

	extensionPathPrefix := postgres.ExtensionsBaseDirectory + "/"
	if strings.HasPrefix(mount.MountPath, extensionPathPrefix) && !strings.HasPrefix(name, "ext-") {
		return SanitizeExtensionNameForVolume(name)
	}

	if strings.HasPrefix(mount.MountPath, PgTablespaceVolumePath+"/") && !strings.HasPrefix(name, "tbs-") {
		return "tbs-" + name
	}

	return name
}

func compareVolumes(currentVolumes, targetVolumes []corev1.Volume) (bool, string) {
	current := make(map[string]corev1.Volume)
	target := make(map[string]corev1.Volume)
	for _, vol := range currentVolumes {
		if shouldIgnoreCurrentVolume(vol.Name) {
			continue
		}
		normalized := normalizeVolumeName(vol)
		vol.Name = normalized
		current[normalized] = vol
	}
	for _, vol := range targetVolumes {
		target[vol.Name] = vol
	}

	return compareMaps(current, target)
}

func compareVolumeMounts(currentMounts, targetMounts []corev1.VolumeMount) (bool, string) {
	current := make(map[string]corev1.VolumeMount)
	target := make(map[string]corev1.VolumeMount)
	for _, mount := range currentMounts {
		if shouldIgnoreCurrentVolume(mount.Name) {
			continue
		}
		normalized := normalizeVolumeMountName(mount)
		mount.Name = normalized
		current[normalized] = mount
	}
	for _, mount := range targetMounts {
		target[mount.Name] = mount
	}

	return compareMaps(current, target)
}

// doContainersMatch checks if the containers match. They are assumed to be for the same name.
// If they don't match, the first diff found is returned
func doContainersMatch(currentContainer, targetContainer corev1.Container) (bool, string) {
	comparisons := map[string]func() bool{
		"image": func() bool {
			return currentContainer.Image == targetContainer.Image
		},
		"environment": func() bool {
			return EnvConfig{
				EnvFrom: currentContainer.EnvFrom,
				EnvVars: currentContainer.Env,
			}.IsEnvEqual(targetContainer)
		},
		"readiness-probe": func() bool {
			return reflect.DeepEqual(currentContainer.ReadinessProbe, targetContainer.ReadinessProbe)
		},
		"liveness-probe": func() bool {
			return reflect.DeepEqual(currentContainer.LivenessProbe, targetContainer.LivenessProbe)
		},
		"startup-probe": func() bool {
			return reflect.DeepEqual(currentContainer.StartupProbe, targetContainer.StartupProbe)
		},
		"command": func() bool {
			return reflect.DeepEqual(currentContainer.Command, targetContainer.Command)
		},
		"resources": func() bool {
			// semantic equality will compare the two objects semantically, not only numbers
			return equality.Semantic.DeepEqual(
				currentContainer.Resources,
				targetContainer.Resources,
			)
		},
		"ports": func() bool {
			return reflect.DeepEqual(currentContainer.Ports, targetContainer.Ports)
		},
		"security-context": func() bool {
			return reflect.DeepEqual(currentContainer.SecurityContext, targetContainer.SecurityContext)
		},
	}

	for diff, f := range comparisons {
		if !f() {
			return false, diff
		}
	}

	match, diff := compareVolumeMounts(currentContainer.VolumeMounts, targetContainer.VolumeMounts)
	if !match {
		return false, "volume-mounts: " + diff
	}
	return true, ""
}

func compareContainers(currentContainers, targetContainers []corev1.Container) (bool, string) {
	current := make(map[string]corev1.Container)
	target := make(map[string]corev1.Container)
	for _, c := range currentContainers {
		current[c.Name] = c
	}
	for _, c := range targetContainers {
		target[c.Name] = c
	}
	for name, currentContainer := range current {
		container2, found := target[name]
		if !found {
			return false, fmt.Sprintf("container %s has been removed", name)
		}
		match, diff := doContainersMatch(currentContainer, container2)
		if !match {
			return false, fmt.Sprintf("container %s differs in %s", name, diff)
		}
	}
	for name := range target {
		_, found := current[name]
		if !found {
			return false, fmt.Sprintf("container %s has been added", name)
		}
	}
	return true, ""
}
