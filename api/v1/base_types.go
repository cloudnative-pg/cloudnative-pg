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

package v1

import (
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PodStatus represent the possible status of pods
type PodStatus string

const (
	// PodHealthy means that a Pod is active and ready
	PodHealthy = "healthy"

	// PodReplicating means that a Pod is still not ready but still active
	PodReplicating = "replicating"

	// PodFailed means that a Pod will not be scheduled again (deleted or evicted)
	PodFailed = "failed"
)

// LocalObjectReference contains enough information to let you locate a
// local object with a known type inside the same namespace
// +kubebuilder:object:generate:=false
type LocalObjectReference = machineryapi.LocalObjectReference

// SecretKeySelector contains enough information to let you locate
// the key of a Secret
// +kubebuilder:object:generate:=false
type SecretKeySelector = machineryapi.SecretKeySelector

// ConfigMapKeySelector contains enough information to let you locate
// the key of a ConfigMap
// +kubebuilder:object:generate:=false
type ConfigMapKeySelector = machineryapi.ConfigMapKeySelector

// SecretKeySelectorToCore transforms a SecretKeySelector structure to the
// analogue one in the corev1 namespace
func SecretKeySelectorToCore(selector *SecretKeySelector) *corev1.SecretKeySelector {
	if selector == nil {
		return nil
	}

	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: selector.LocalObjectReference.Name,
		},
		Key: selector.Key,
	}
}

// ConfigMapKeySelectorToCore transforms a ConfigMapKeySelector structure to the analogue
// one in the corev1 namespace
func ConfigMapKeySelectorToCore(selector *ConfigMapKeySelector) *corev1.ConfigMapKeySelector {
	if selector == nil {
		return nil
	}

	return &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: selector.Name,
		},
		Key: selector.Key,
	}
}

// ListStatusPods return a list of active Pods
func ListStatusPods(podList []corev1.Pod) map[PodStatus][]string {
	podsNames := make(map[PodStatus][]string)

	for _, pod := range podList {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		switch {
		case utils.IsPodReady(pod):
			podsNames[PodHealthy] = append(podsNames[PodHealthy], pod.Name)
		case utils.IsPodActive(pod):
			podsNames[PodReplicating] = append(podsNames[PodReplicating], pod.Name)
		default:
			podsNames[PodFailed] = append(podsNames[PodFailed], pod.Name)
		}
	}

	return podsNames
}
