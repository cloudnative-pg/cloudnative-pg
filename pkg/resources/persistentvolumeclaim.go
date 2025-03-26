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

package resources

import (
	corev1 "k8s.io/api/core/v1"
)

// PersistentVolumeClaimBuilder creates a fluent abstraction to interact with the kubernetes resources
type PersistentVolumeClaimBuilder struct {
	pvc *corev1.PersistentVolumeClaim
}

// NewPersistentVolumeClaimBuilder instantiates an empty PersistentVolumeClaimBuilder
func NewPersistentVolumeClaimBuilder() *PersistentVolumeClaimBuilder {
	return &PersistentVolumeClaimBuilder{pvc: &corev1.PersistentVolumeClaim{}}
}

// NewPersistentVolumeClaimBuilderFromPVC instantiates a builder with an existing object
func NewPersistentVolumeClaimBuilderFromPVC(pvc *corev1.PersistentVolumeClaim) *PersistentVolumeClaimBuilder {
	return &PersistentVolumeClaimBuilder{pvc: pvc}
}

// WithSpec assigns the currently passed specs to the underlying object
func (b *PersistentVolumeClaimBuilder) WithSpec(spec *corev1.PersistentVolumeClaimSpec) *PersistentVolumeClaimBuilder {
	if spec == nil {
		return b
	}

	b.pvc.Spec = *spec
	return b
}

// WithSource assigns the currently source to the underlying object
func (b *PersistentVolumeClaimBuilder) WithSource(
	source *corev1.TypedLocalObjectReference,
) *PersistentVolumeClaimBuilder {
	if source == nil {
		return b
	}

	b.pvc.Spec.DataSource = source.DeepCopy()
	return b
}

// WithStorageClass adds the storageClass to the object being build
func (b *PersistentVolumeClaimBuilder) WithStorageClass(storageClass *string) *PersistentVolumeClaimBuilder {
	b.pvc.Spec.StorageClassName = storageClass
	return b
}

// WithRequests adds the requests to the object being build
func (b *PersistentVolumeClaimBuilder) WithRequests(rl corev1.ResourceList) *PersistentVolumeClaimBuilder {
	b.pvc.Spec.Resources.Requests = rl
	return b
}

// WithDefaultAccessMode adds the access mode only if it was not present in the initial PersistentVolumeSpec
func (b *PersistentVolumeClaimBuilder) WithDefaultAccessMode(
	accessMode corev1.PersistentVolumeAccessMode,
) *PersistentVolumeClaimBuilder {
	if len(b.pvc.Spec.AccessModes) > 0 {
		return b
	}

	b.pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
		accessMode,
	}
	return b
}

// BeginMetadata gets the metadata builder
func (b *PersistentVolumeClaimBuilder) BeginMetadata() *ResourceMetadataBuilder[*PersistentVolumeClaimBuilder] {
	return NewResourceMetadataBuilder(&b.pvc.ObjectMeta, b)
}

// Build returns the underlying object
func (b *PersistentVolumeClaimBuilder) Build() *corev1.PersistentVolumeClaim {
	return b.pvc
}
