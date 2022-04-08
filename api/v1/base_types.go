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
	corev1 "k8s.io/api/core/v1"
)

// LocalObjectReference contains enough information to let you locate a
// local object with a known type inside the same namespace
type LocalObjectReference struct {
	// Name of the referent.
	Name string `json:"name"`
}

// SecretKeySelector contains enough information to let you locate
// the key of a Secret
type SecretKeySelector struct {
	// The name of the secret in the pod's namespace to select from.
	LocalObjectReference `json:",inline"`
	// The key to select
	Key string `json:"key"`
}

// ConfigMapKeySelector contains enough information to let you locate
// the key of a ConfigMap
type ConfigMapKeySelector struct {
	// The name of the secret in the pod's namespace to select from.
	LocalObjectReference `json:",inline"`
	// The key to select
	Key string `json:"key"`
}

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
