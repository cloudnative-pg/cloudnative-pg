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
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
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
