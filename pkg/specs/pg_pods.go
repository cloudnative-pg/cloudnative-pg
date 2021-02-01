/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetNodeSerial get the serial number of an object created by the operator
// for a Cluster
func GetNodeSerial(object metav1.ObjectMeta) (int, error) {
	nodeSerial, ok := object.Annotations[ClusterSerialAnnotationName]
	if !ok {
		return 0, fmt.Errorf("missing node serial annotation")
	}

	result, err := strconv.Atoi(nodeSerial)
	if err != nil {
		return 0, fmt.Errorf("wrong node serial annotation: %v", nodeSerial)
	}

	return result, nil
}

// IsPodPrimary check if a certain pod belongs to a primary
func IsPodPrimary(pod corev1.Pod) bool {
	role, hasRole := pod.ObjectMeta.Labels[ClusterRoleLabelName]
	if !hasRole {
		return false
	}

	return role == ClusterRoleLabelPrimary
}

// IsPodStandby check if a certain pod belongs to a standby
func IsPodStandby(pod corev1.Pod) bool {
	return !IsPodPrimary(pod)
}

// GetPostgresImageName get the PostgreSQL image name used in this Pod
func GetPostgresImageName(pod corev1.Pod) (string, error) {
	return GetContainerImageName(pod, PostgresContainerName)
}

// GetBootstrapControllerImageName get the controller image name used to bootstrap a Pod
func GetBootstrapControllerImageName(pod corev1.Pod) (string, error) {
	return GetInitContainerImageName(pod, BootstrapControllerContainerName)
}

// GetContainerImageName get the name of the image used in a container
func GetContainerImageName(pod corev1.Pod, containerName string) (string, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return container.Image, nil
		}
	}

	return "", fmt.Errorf("container %q not found", containerName)
}

// GetInitContainerImageName get the name of the image used in an init container
func GetInitContainerImageName(pod corev1.Pod, containerName string) (string, error) {
	for _, container := range pod.Spec.InitContainers {
		if container.Name == containerName {
			return container.Image, nil
		}
	}

	return "", fmt.Errorf("init container %q not found", containerName)
}
