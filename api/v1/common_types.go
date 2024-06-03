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

import "k8s.io/apimachinery/pkg/util/validation/field"

// VolumeSnapshotKind this is a strongly typed reference to the kind used by the volumesnapshot package
const VolumeSnapshotKind = "VolumeSnapshot"

// Metadata is a structure similar to the metav1.ObjectMeta, but still
// parseable by controller-gen to create a suitable CRD for the user.
// The comment of PodTemplateSpec has an explanation of why we are
// not using the core data types.
type Metadata struct {
	// The name of the resource. Only supported for certain types
	Name string `json:"name,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WebhookValidateNameAsRequired checks if the Name field in Metadata is set.
// It returns nil if the Name is not empty, indicating the validation passed.
// Otherwise, it returns a field.Error indicating that the Name is required.
//
// Parameters:
// - path: the field.Path to the Name field.
//
// Returns:
// - *field.Error: an error if the Name is not specified, otherwise nil.
func (m Metadata) WebhookValidateNameAsRequired(path *field.Path) *field.Error {
	if m.Name != "" {
		return nil
	}

	return field.Required(path, "must specify name")
}

// WebhookValidateNameAsProhibited checks if the Name field in Metadata is not set.
// It returns nil if the Name is empty, indicating the validation passed.
// Otherwise, it returns a field.Error indicating that the Name is not supported.
//
// Parameters:
// - path: the field.Path to the Name field.
//
// Returns:
// - *field.Error: an error if the Name is specified, otherwise nil.
func (m Metadata) WebhookValidateNameAsProhibited(path *field.Path) *field.Error {
	if m.Name == "" {
		return nil
	}

	return field.Invalid(path, m.Name, "name field not supported")
}
