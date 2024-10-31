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

// PublicationReclaimPolicy describes a policy for end-of-life maintenance of Publications.
// +enum
type PublicationReclaimPolicy string

const (
	// PublicationReclaimDelete means the publication will be deleted from Kubernetes on release
	// from its claim.
	PublicationReclaimDelete PublicationReclaimPolicy = "delete"

	// PublicationReclaimRetain means the publication will be left in its current phase for manual
	// reclamation by the administrator. The default policy is Retain.
	PublicationReclaimRetain PublicationReclaimPolicy = "retain"
)

// PublicationSpec defines the desired state of Publication
type PublicationSpec struct {
	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The name inside PostgreSQL
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name"`

	// The name of the database
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="dbname is immutable"
	DBName string `json:"dbname"`

	// Parameters
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// Publication target
	Target PublicationTarget `json:"target"`

	// The policy for end-of-life maintenance of this publication
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy PublicationReclaimPolicy `json:"publicationReclaimPolicy,omitempty"`
}

// PublicationTarget is what this publication should publish
// +kubebuilder:validation:XValidation:rule="(has(self.allTables) && !has(self.objects)) || (!has(self.allTables) && has(self.objects))",message="allTables and objects are mutually exclusive"
type PublicationTarget struct {
	// All tables should be published
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="allTables is immutable"
	// +optional
	AllTables bool `json:"allTables,omitempty"`

	// Just the following schema objects
	// +kubebuilder:validation:XValidation:rule="!(self.exists(o, has(o.table) && has(o.table.columns)) && self.exists(o, has(o.tablesInSchema)))",message="specifying a column list when the publication also publishes tablesInSchema is not supported"
	// +kubebuilder:validation:MaxItems=100000
	// +optional
	Objects []PublicationTargetObject `json:"objects,omitempty"`
}

// PublicationTargetObject is an object to publish
// +kubebuilder:validation:XValidation:rule="(has(self.tablesInSchema) && !has(self.table)) || (!has(self.tablesInSchema) && has(self.table))",message="tablesInSchema and table are mutually exclusive"
type PublicationTargetObject struct {
	// The schema to publish
	// +optional
	TablesInSchema string `json:"tablesInSchema,omitempty"`

	// A table to publish
	// +optional
	Table *PublicationTargetTable `json:"table,omitempty"`
}

// PublicationTargetTable is a table to publish
type PublicationTargetTable struct {
	// Whether to limit to the table only or include all its descendants
	// +optional
	Only bool `json:"only,omitempty"`

	// The table name
	Name string `json:"name"`

	// The schema name
	// +optional
	Schema string `json:"schema,omitempty"`

	// The columns to publish
	// +optional
	Columns []string `json:"columns,omitempty"`
}

// PublicationStatus defines the observed state of Publication
type PublicationStatus struct {
	// A sequence number representing the latest
	// desired state that was synchronized
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Applied is true if the publication was reconciled correctly
	// +optional
	Applied *bool `json:"applied,omitempty"`

	// Message is the reconciliation output message
	// +optional
	Message string `json:"message,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Applied",type="boolean",JSONPath=".status.applied"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Latest reconciliation message"

// Publication is the Schema for the publications API
type Publication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PublicationSpec   `json:"spec"`
	Status PublicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PublicationList contains a list of Publication
type PublicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Publication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Publication{}, &PublicationList{})
}
