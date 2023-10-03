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
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetNodeSerial get the serial number of an object created by the operator
// for a Cluster
func GetNodeSerial(object metav1.ObjectMeta) (int, error) {
	nodeSerial, ok := object.Annotations[utils.ClusterSerialAnnotationName]
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
	return IsPrimary(pod.ObjectMeta)
}

// IsPrimary check if a certain resource belongs to a primary
func IsPrimary(meta metav1.ObjectMeta) bool {
	role, hasRole := utils.GetInstanceRole(meta.Labels)
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
