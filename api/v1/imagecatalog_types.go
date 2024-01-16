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
}

// CatalogImage defines the image and major version
type CatalogImage struct {
	// The image reference
	Image string `json:"image"`
	// +kubebuilder:validation:Minimum=10
	// The PostgreSQL major version of the image. Must be unique within the catalog.
	Major int `json:"major"`
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

// GetObjectMeta returns the ObjectMeta of the ImageCatalog
func (c *ImageCatalog) GetObjectMeta() *metav1.ObjectMeta {
	return &c.ObjectMeta
}

// GetSpec returns the Spec of the ImageCatalog
func (c *ImageCatalog) GetSpec() *ImageCatalogSpec {
	return &c.Spec
}

func init() {
	SchemeBuilder.Register(&ImageCatalog{}, &ImageCatalogList{})
}
