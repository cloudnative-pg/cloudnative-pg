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

// DatabaseSpec is the specification of a Postgresql Database, built around the
// `CREATE DATABASE`, `ALTER DATABASE`, and `DROP DATABASE` SQL commands of
// PostgreSQL.
// +kubebuilder:validation:XValidation:rule="!has(self.builtinLocale) || self.localeProvider == 'builtin'",message="builtinLocale is only available when localeProvider is set to `builtin`"
// +kubebuilder:validation:XValidation:rule="!has(self.icuLocale) || self.localeProvider == 'icu'",message="icuLocale is only available when localeProvider is set to `icu`"
// +kubebuilder:validation:XValidation:rule="!has(self.icuRules) || self.localeProvider == 'icu'",message="icuRules is only available when localeProvider is set to `icu`"
type DatabaseSpec struct {
	// The name of the PostgreSQL cluster hosting the database.
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// Ensure the PostgreSQL database is `present` or `absent` - defaults to "present".
	// +kubebuilder:default:="present"
	// +kubebuilder:validation:Enum=present;absent
	// +optional
	Ensure EnsureOption `json:"ensure,omitempty"`

	// The name of the database to create inside PostgreSQL. This setting cannot be changed.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	// +kubebuilder:validation:XValidation:rule="self != 'postgres'",message="the name postgres is reserved"
	// +kubebuilder:validation:XValidation:rule="self != 'template0'",message="the name template0 is reserved"
	// +kubebuilder:validation:XValidation:rule="self != 'template1'",message="the name template1 is reserved"
	Name string `json:"name"`

	// Maps to the `OWNER` parameter of `CREATE DATABASE`.
	// Maps to the `OWNER TO` command of `ALTER DATABASE`.
	// The role name of the user who owns the database inside PostgreSQL.
	Owner string `json:"owner"`

	// Maps to the `TEMPLATE` parameter of `CREATE DATABASE`. This setting
	// cannot be changed. The name of the template from which to create
	// this database.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="template is immutable"
	Template string `json:"template,omitempty"`

	// Maps to the `ENCODING` parameter of `CREATE DATABASE`. This setting
	// cannot be changed. Character set encoding to use in the database.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="encoding is immutable"
	// +optional
	Encoding string `json:"encoding,omitempty"`

	// Maps to the `LOCALE` parameter of `CREATE DATABASE`. This setting
	// cannot be changed. Sets the default collation order and character
	// classification in the new database.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="locale is immutable"
	// +optional
	Locale string `json:"locale,omitempty"`

	// Maps to the `LOCALE_PROVIDER` parameter of `CREATE DATABASE`. This
	// setting cannot be changed. This option sets the locale provider for
	// databases created in the new cluster. Available from PostgreSQL 16.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="localeProvider is immutable"
	// +optional
	LocaleProvider string `json:"localeProvider,omitempty"`

	// Maps to the `LC_COLLATE` parameter of `CREATE DATABASE`. This
	// setting cannot be changed.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="localeCollate is immutable"
	// +optional
	LcCollate string `json:"localeCollate,omitempty"`

	// Maps to the `LC_CTYPE` parameter of `CREATE DATABASE`. This setting
	// cannot be changed.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="localeCType is immutable"
	// +optional
	LcCtype string `json:"localeCType,omitempty"`

	// Maps to the `ICU_LOCALE` parameter of `CREATE DATABASE`. This
	// setting cannot be changed. Specifies the ICU locale when the ICU
	// provider is used. This option requires `localeProvider` to be set to
	// `icu`. Available from PostgreSQL 15.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="icuLocale is immutable"
	// +optional
	IcuLocale string `json:"icuLocale,omitempty"`

	// Maps to the `ICU_RULES` parameter of `CREATE DATABASE`. This setting
	// cannot be changed. Specifies additional collation rules to customize
	// the behavior of the default collation. This option requires
	// `localeProvider` to be set to `icu`. Available from PostgreSQL 16.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="icuRules is immutable"
	// +optional
	IcuRules string `json:"icuRules,omitempty"`

	// Maps to the `BUILTIN_LOCALE` parameter of `CREATE DATABASE`. This
	// setting cannot be changed. Specifies the locale name when the
	// builtin provider is used. This option requires `localeProvider` to
	// be set to `builtin`. Available from PostgreSQL 17.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="builtinLocale is immutable"
	// +optional
	BuiltinLocale string `json:"builtinLocale,omitempty"`

	// Maps to the `COLLATION_VERSION` parameter of `CREATE DATABASE`. This
	// setting cannot be changed.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="collationVersion is immutable"
	// +optional
	CollationVersion string `json:"collationVersion,omitempty"`

	// Maps to the `IS_TEMPLATE` parameter of `CREATE DATABASE` and `ALTER
	// DATABASE`. If true, this database is considered a template and can
	// be cloned by any user with `CREATEDB` privileges.
	// +optional
	IsTemplate *bool `json:"isTemplate,omitempty"`

	// Maps to the `ALLOW_CONNECTIONS` parameter of `CREATE DATABASE` and
	// `ALTER DATABASE`. If false then no one can connect to this database.
	// +optional
	AllowConnections *bool `json:"allowConnections,omitempty"`

	// Maps to the `CONNECTION LIMIT` clause of `CREATE DATABASE` and
	// `ALTER DATABASE`. How many concurrent connections can be made to
	// this database. -1 (the default) means no limit.
	// +optional
	ConnectionLimit *int `json:"connectionLimit,omitempty"`

	// Maps to the `TABLESPACE` parameter of `CREATE DATABASE`.
	// Maps to the `SET TABLESPACE` command of `ALTER DATABASE`.
	// The name of the tablespace (in PostgreSQL) that will be associated
	// with the new database. This tablespace will be the default
	// tablespace used for objects created in this database.
	// +optional
	Tablespace string `json:"tablespace,omitempty"`

	// The policy for end-of-life maintenance of this database.
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy DatabaseReclaimPolicy `json:"databaseReclaimPolicy,omitempty"`

	// The list of extensions to be managed in the database
	// +optional
	Extensions []ExtensionSpec `json:"extensions,omitempty"`
}

// ExtensionSpec configures an extension in a database
type ExtensionSpec struct {
	// Name is the name of the extension
	Name string `json:"name,omitempty"`

	// Ensure tells the operator to install or remove an extension from
	// the database
	// +kubebuilder:default:="present"
	// +kubebuilder:validation:Enum=present;absent
	// +optional
	Ensure EnsureOption `json:"ensure"`

	// Version is the version of extension to be installed.
	// If empty the operator will install the default version and not update it.
	Version string `json:"version,omitempty"`

	// Schema is the schema where the extension will be installed.
	// Defaults to the default extension schema.
	Schema string `json:"schema,omitempty"`
}

// DatabaseStatus defines the observed state of Database
type DatabaseStatus struct {
	// A sequence number representing the latest
	// desired state that was synchronized
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Applied is true if the database was reconciled correctly
	// +optional
	Applied *bool `json:"applied,omitempty"`

	// Message is the reconciliation output message
	// +optional
	Message string `json:"message,omitempty"`

	// Extensions is the status of the managed extensions
	// +optional
	Extensions []ExtensionStatus `json:"extensions,omitempty"`
}

// ExtensionStatus is the status of the managed extensions
type ExtensionStatus struct {
	// The name of the extension
	Name string `json:"name"`

	// True of the extension has been installed successfully in
	// the database
	Applied bool `json:"applied"`

	// Message is the extension reconciliation message
	// +optional
	Message string `json:"message,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Applied",type="boolean",JSONPath=".status.applied"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Latest reconciliation message"

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
