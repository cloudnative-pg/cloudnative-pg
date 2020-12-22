/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import corev1 "k8s.io/api/core/v1"

// UpsertContainers merge two list of containers
func UpsertContainers(base []corev1.Container, changes []corev1.Container) []corev1.Container {
	result := make([]corev1.Container, len(base))
	copy(result, base)

	nameMap := make(map[string]int)
	for idx, container := range base {
		nameMap[container.Name] = idx
	}

	for idx := range changes {
		baseIdx, isInBase := nameMap[changes[idx].Name]
		if isInBase {
			result[baseIdx] = changes[idx]
		} else {
			result = append(result, changes[idx])
			nameMap[changes[idx].Name] = len(result) - 1
		}
	}

	return result
}

// UpsertVolumes merge two list of Volumes
func UpsertVolumes(base []corev1.Volume, changes []corev1.Volume) []corev1.Volume {
	result := make([]corev1.Volume, len(base))
	copy(result, base)

	nameMap := make(map[string]int)
	for idx, Volume := range base {
		nameMap[Volume.Name] = idx
	}

	for idx := range changes {
		baseIdx, isInBase := nameMap[changes[idx].Name]
		if isInBase {
			result[baseIdx] = changes[idx]
		} else {
			result = append(result, changes[idx])
			nameMap[changes[idx].Name] = len(result) - 1
		}
	}

	return result
}
