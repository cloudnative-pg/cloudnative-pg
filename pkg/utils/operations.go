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

package utils

import (
	corev1 "k8s.io/api/core/v1"
)

// CollectDifferencesFromMaps returns a map of the differences (as slice of strings) of the values of two given maps.
// Map result values are added when a key is present just in one of the input maps, or if the values are different
// given the same key
func CollectDifferencesFromMaps(p1 map[string]string, p2 map[string]string) map[string][]string {
	diff := make(map[string][]string)
	totalKeys := make(map[string]bool)
	for k := range p1 {
		totalKeys[k] = true
	}
	for k := range p2 {
		totalKeys[k] = true
	}
	for k := range totalKeys {
		v1, ok1 := p1[k]
		v2, ok2 := p2[k]
		if ok1 && ok2 && v1 == v2 {
			continue
		}
		diff[k] = []string{v1, v2}
	}
	if len(diff) > 0 {
		return diff
	}
	return nil
}

// isMapSubset returns true if mapSubset is a subset of mapSet otherwise false
func isMapSubset(mapSet map[string]string, mapSubset map[string]string) bool {
	if len(mapSet) < len(mapSubset) {
		return false
	}

	if len(mapSubset) == 0 {
		return true
	}

	for subMapKey, subMapValue := range mapSubset {
		mapValue := mapSet[subMapKey]

		if mapValue != subMapValue {
			return false
		}
	}

	return true
}

// isResourceListSubset returns true if subResourceList is a subset of resourceList otherwise false
func isResourceListSubset(resourceList, subResourceList corev1.ResourceList) bool {
	if len(resourceList) < len(subResourceList) {
		return false
	}

	if len(subResourceList) == 0 {
		return true
	}

	for key, subValue := range subResourceList {
		value := resourceList[key]

		if !subValue.Equal(value) {
			return false
		}
	}

	return true
}

// IsLabelSubset checks if a collection of labels is a subset of another
//
// NOTE: there are two parameters for the labels to check. The `fixed` one
// is for labels that certainly should be inherited (`inheritedMetadata` in the spec)
// The other labels may or may not be inherited depending on the configuration
func IsLabelSubset(
	mapSet,
	clusterLabels,
	fixedInheritedLabels map[string]string,
	controller InheritanceController,
) bool {
	mapToEvaluate := map[string]string{}

	for key, value := range fixedInheritedLabels {
		mapToEvaluate[key] = value
	}

	for key, value := range clusterLabels {
		if controller.IsLabelInherited(key) {
			mapToEvaluate[key] = value
		}
	}

	return isMapSubset(mapSet, mapToEvaluate)
}

// IsAnnotationSubset checks if a collection of annotations is a subset of another
//
// NOTE: there are two parameters for the annotations to check. The `fixed` one
// is for annotations that certainly should be inherited (`inheritedMetadata` in the spec)
// The other annotations may or may not be inherited depending on the configuration
func IsAnnotationSubset(
	mapSet, clusterAnnotations, fixedInheritedAnnotations map[string]string,
	controller InheritanceController,
) bool {
	mapToEvaluate := map[string]string{}

	for key, value := range fixedInheritedAnnotations {
		mapToEvaluate[key] = value
	}

	for key, value := range clusterAnnotations {
		if controller.IsAnnotationInherited(key) {
			mapToEvaluate[key] = value
		}
	}

	return isMapSubset(mapSet, mapToEvaluate)
}

// IsResourceSubset checks if some resource requirements are a subset of another
func IsResourceSubset(resources, resourcesSubset corev1.ResourceRequirements) bool {
	return isResourceListSubset(resources.Requests, resourcesSubset.Requests) &&
		isResourceListSubset(resources.Limits, resourcesSubset.Limits)
}
