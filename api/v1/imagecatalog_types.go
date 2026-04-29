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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImageCatalogSpec defines the desired ImageCatalog
type ImageCatalogSpec struct {
	// List of CatalogImages available in the catalog
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(e, self.filter(f, f.major==e.major).size() == 1)",message=Images must have unique major versions
	Images []CatalogImage `json:"images"`

	// ExtraImages is a list of named images for components other than PostgreSQL
	// (e.g. pgbouncer). Keys must be unique within a catalog.
	// +optional
	// +listType=map
	// +listMapKey=key
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(e, self.filter(f, f.key==e.key).size() == 1)",message="Extra image keys must be unique"
	ExtraImages []CatalogExtraImage `json:"extraImages,omitempty"`
}

// CatalogExtraImage is a named image entry for a non-PostgreSQL component.
type CatalogExtraImage struct {
	// Key is the unique identifier for this image within the catalog.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	Key string `json:"key"`

	// Image is the container image reference.
	Image string `json:"image"`
}

// CatalogImage defines the image and major version
type CatalogImage struct {
	// The image reference
	Image string `json:"image"`
	// +kubebuilder:validation:Minimum=10
	// The PostgreSQL major version of the image. Must be unique within the catalog.
	Major int `json:"major"`
	// The configuration of the extensions to be added
	// +optional
	// +listType=map
	// +listMapKey=name
	Extensions []ExtensionConfiguration `json:"extensions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ImageCatalog is the Schema for the imagecatalogs API
type ImageCatalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	// Specification of the desired behavior of the ImageCatalog.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec ImageCatalogSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// ImageCatalogList contains a list of ImageCatalog
type ImageCatalogList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	metav1.ListMeta `json:"metadata"`
	// List of ImageCatalogs
	Items []ImageCatalog `json:"items"`
}
