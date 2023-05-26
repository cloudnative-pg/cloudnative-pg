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

// WithAccessModes adds the access modes to the object being build
func (b *PersistentVolumeClaimBuilder) WithAccessModes(
	accessModes ...corev1.PersistentVolumeAccessMode,
) *PersistentVolumeClaimBuilder {
	b.pvc.Spec.AccessModes = append(b.pvc.Spec.AccessModes, accessModes...)
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
