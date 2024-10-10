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

// DatabaseReclaimPolicy describes a policy for end-of-life maintenance of databases.
// +enum
type DatabaseReclaimPolicy string

const (
	// DatabaseReclaimDelete means the database will be deleted from its PostgreSQL Cluster on release
	// from its claim.
	DatabaseReclaimDelete DatabaseReclaimPolicy = "delete"

	// DatabaseReclaimRetain means the database will be left in its current phase for manual
	// reclamation by the administrator. The default policy is Retain.
	DatabaseReclaimRetain DatabaseReclaimPolicy = "retain"
)

// DatabaseSpec is the specification of a Postgresql Database
type DatabaseSpec struct {
	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The name inside PostgreSQL
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	// +kubebuilder:validation:XValidation:rule="self != 'postgres'",message="the name postgres is reserved"
	// +kubebuilder:validation:XValidation:rule="self != 'template0'",message="the name template0 is reserved"
	// +kubebuilder:validation:XValidation:rule="self != 'template1'",message="the name template1 is reserved"
	Name string `json:"name"`

	// The owner
	Owner string `json:"owner"`

	// The name of the template from which to create the new database
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="template is immutable"
	Template string `json:"template,omitempty"`

	// The encoding (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="encoding is immutable"
	// +optional
	Encoding string `json:"encoding,omitempty"`

	// The locale (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="locale is immutable"
	// +optional
	Locale string `json:"locale,omitempty"`

	// The locale provider (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="locale_provider is immutable"
	// +optional
	LocaleProvider string `json:"locale_provider,omitempty"`

	// The LC_COLLATE (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="lc_collate is immutable"
	// +optional
	LcCollate string `json:"lc_collate,omitempty"`

	// The LC_CTYPE (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="lc_ctype is immutable"
	// +optional
	LcCtype string `json:"lc_ctype,omitempty"`

	// The ICU_LOCALE (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="icu_locale is immutable"
	// +optional
	IcuLocale string `json:"icu_locale,omitempty"`

	// The ICU_RULES (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="icu_rules is immutable"
	// +optional
	IcuRules string `json:"icu_rules,omitempty"`

	// The BUILTIN_LOCALE (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="builtin_locale is immutable"
	// +optional
	BuiltinLocale string `json:"builtin_locale,omitempty"`

	// The COLLATION_VERSION (cannot be changed)
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="collation_version is immutable"
	// +optional
	CollationVersion string `json:"collation_version,omitempty"`

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

	// The policy for end-of-life maintenance of this database
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy DatabaseReclaimPolicy `json:"databaseReclaimPolicy,omitempty"`
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
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.error",description="Latest error message"

// Database is the Schema for the databases API
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the desired Database.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec DatabaseSpec `json:"spec"`
	// Most recently observed status of the Database. This data may not be up to
	// date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
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
