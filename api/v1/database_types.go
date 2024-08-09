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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DatabaseSpec is the specification of a Postgresql Database
type DatabaseSpec struct {
	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The name inside PostgreSQL
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name"`

	// The owner
	Owner string `json:"owner"`

	// The encoding (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="encoding is immutable"
	// +optional
	Encoding string `json:"encoding,omitempty"`

	// True when the database is a template
	// +optional
	IsTemplate *bool `json:"isTemplate,omitempty"`

	// True when connections to this database are allowed
	// +optional
	AllowConnections *bool `json:"allowConnections,omitempty"`

	// Connection limit, -1 means no limit and -2 means the
	// database is not valid
	// +optional
	ConnectionLimit *int `json:"connectionLimit,omitempty"`

	// The default tablespace of this database
	// +optional
	Tablespace string `json:"tablespace,omitempty"`
}

// DatabaseStatus defines the observed state of Database
type DatabaseStatus struct {
	// A sequence number representing the latest
	// desired state that was synchronized
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Ready is true if the database was reconciled correctly
	Ready bool `json:"ready,omitempty"`

	// Error is the reconciliation error message
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.error",description="Latest error message"

// Database is the Schema for the databases API
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseSpec   `json:"spec,omitempty"`
	Status DatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseList contains a list of Database
type DatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Database `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}
