package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:generate=false

// GenericImageCatalog is an interface used to manage ClusterImageCatalog and ImageCatalog in the same way
type GenericImageCatalog interface {
	runtime.Object
	metav1.Object

	// GetObjectMeta returns the ObjectMeta of the GenericImageCatalog
	GetObjectMeta() *metav1.ObjectMeta
	// GetSpec returns the Spec of the GenericImageCatalog
	GetSpec() *ImageCatalogSpec
}
