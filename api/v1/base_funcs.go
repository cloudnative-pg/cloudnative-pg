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

package v1

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// SecretKeySelectorToCore transforms a SecretKeySelector structure to the
// analogue one in the corev1 namespace
func SecretKeySelectorToCore(selector *SecretKeySelector) *corev1.SecretKeySelector {
	if selector == nil {
		return nil
	}

	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: selector.Name,
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
