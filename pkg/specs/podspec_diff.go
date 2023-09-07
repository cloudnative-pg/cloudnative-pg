/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package specs

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

// ComparePodSpecs compares two pod specs, returns true iff they are equivalent, and
// if they are not, points out the first discrepancy.
// This function matches CreateClusterPodSpec, specifically it looks in more detail
// and ignores reordering of volume mounts and containers
func ComparePodSpecs(
	podSpec1, podSpec2 corev1.PodSpec,
) (bool, string) {
	comparisons := map[string]func() (bool, string){
		"volumes": func() (bool, string) {
			return compareVolumes(podSpec1.Volumes, podSpec2.Volumes)
		},
		"containers": func() (bool, string) {
			return compareContainers(podSpec1.Containers, podSpec2.Containers)
		},
		"init-containers": func() (bool, string) {
			return compareContainers(podSpec1.InitContainers, podSpec2.InitContainers)
		},
	}

	for comp, f := range comparisons {
		areEqual, diff := f()
		if areEqual {
			continue
		}
		return false, fmt.Sprintf("podSpecs differ on %s: %s", comp, diff)
	}

	genericComparisons := map[string]func() bool{
		"security-context": func() bool {
			return reflect.DeepEqual(podSpec1.SecurityContext, podSpec2.SecurityContext)
		},
		"affinity": func() bool {
			return reflect.DeepEqual(podSpec1.Affinity, podSpec2.Affinity)
		},
		"tolerations": func() bool {
			return reflect.DeepEqual(podSpec1.Tolerations, podSpec2.Tolerations)
		},
		"node-selector": func() bool {
			return reflect.DeepEqual(podSpec1.NodeSelector, podSpec2.NodeSelector)
		},
		"topology-spread-constraints": func() bool {
			return reflect.DeepEqual(podSpec1.TopologySpreadConstraints, podSpec2.TopologySpreadConstraints)
		},
		"service-account-name": func() bool {
			return podSpec1.ServiceAccountName == podSpec2.ServiceAccountName
		},
		"scheduler-name": func() bool {
			return podSpec1.SchedulerName == podSpec2.SchedulerName
		},
		"hostname": func() bool {
			return podSpec1.Hostname == podSpec2.Hostname
		},
		"termination-grace-period": func() bool {
			return podSpec1.TerminationGracePeriodSeconds == nil && podSpec2.TerminationGracePeriodSeconds == nil ||
				*podSpec1.TerminationGracePeriodSeconds == *podSpec2.TerminationGracePeriodSeconds
		},
	}

	for comp, f := range genericComparisons {
		areEqual := f()
		if areEqual {
			continue
		}
		return false, fmt.Sprintf("podSpecs differ on %s", comp)
	}

	return true, ""
}

// compareMaps returns true iff the maps are equivalent, otherwise returns
// false, and the first difference found
func compareMaps[V comparable](map1, map2 map[string]V) (bool, string) {
	for name1, value1 := range map1 {
		value2, found := map2[name1]
		if !found {
			return false, "element " + name1 + " missing from argument 1"
		}
		deepEqual := reflect.DeepEqual(value1, value2)
		if !deepEqual {
			return false, "element " + name1 + " has differing value"
		}
	}
	for name2 := range map2 {
		_, found := map1[name2]
		if !found {
			return false, "element " + name2 + " missing from argument 2"
		}
		// if the key is in both maps, the values have been compared in the previous loop
	}
	return true, ""
}

func compareVolumes(volumes1, volumes2 []corev1.Volume) (bool, string) {
	volume1map := make(map[string]corev1.Volume)
	volume2map := make(map[string]corev1.Volume)
	for _, vol := range volumes1 {
		volume1map[vol.Name] = vol
	}
	for _, vol := range volumes2 {
		volume2map[vol.Name] = vol
	}

	return compareMaps(volume1map, volume2map)
}

func compareVolumeMounts(mounts1, mounts2 []corev1.VolumeMount) (bool, string) {
	map1 := make(map[string]corev1.VolumeMount)
	map2 := make(map[string]corev1.VolumeMount)
	for _, mount := range mounts1 {
		map1[mount.Name] = mount
	}
	for _, mount := range mounts2 {
		map2[mount.Name] = mount
	}

	return compareMaps(map1, map2)
}

// doContainersMatch checks if the containers match. They are assumed to be for the same name.
// If they don't match, the first diff found is returned
func doContainersMatch(container1, container2 corev1.Container) (bool, string) {
	comparisons := map[string]func() bool{
		"image": func() bool {
			return container1.Image == container2.Image
		},
		"environment": func() bool {
			return EnvConfig{
				EnvFrom: container1.EnvFrom,
				EnvVars: container1.Env,
			}.IsEnvEqual(container2)
		},
		"readiness-probe": func() bool {
			return reflect.DeepEqual(container1.ReadinessProbe, container2.ReadinessProbe)
		},
		"liveness-probe": func() bool {
			return reflect.DeepEqual(container1.LivenessProbe, container2.LivenessProbe)
		},
		"command": func() bool {
			return reflect.DeepEqual(container1.Command, container2.Command)
		},
		"resources": func() bool {
			return reflect.DeepEqual(container1.Resources, container2.Resources)
		},
		"ports": func() bool {
			return reflect.DeepEqual(container1.Ports, container2.Ports)
		},
		"security-context": func() bool {
			return reflect.DeepEqual(container1.SecurityContext, container2.SecurityContext)
		},
	}

	for diff, f := range comparisons {
		if !f() {
			return false, diff + " mismatch"
		}
	}

	match, diff := compareVolumeMounts(container1.VolumeMounts, container2.VolumeMounts)
	if !match {
		return false, " differing VolumeMounts: " + diff
	}
	return true, ""
}

func compareContainers(containers1, containers2 []corev1.Container) (bool, string) {
	map1 := make(map[string]corev1.Container)
	map2 := make(map[string]corev1.Container)
	for _, c := range containers1 {
		map1[c.Name] = c
	}
	for _, c := range containers2 {
		map2[c.Name] = c
	}
	for name1, container1 := range map1 {
		container2, found := map2[name1]
		if !found {
			return false, "container " + name1 + " is missing from argument 2"
		}
		match, diff := doContainersMatch(container1, container2)
		if !match {
			return false, fmt.Sprintf("container %s: %s", name1, diff)
		}
	}
	for name2 := range map2 {
		_, found := map1[name2]
		if !found {
			return false, "container " + name2 + " is missing from argument 1"
		}
	}
	return true, ""
}
