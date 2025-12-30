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

// UsageSpecType describes the type of usage specified in the `usage` field of the
// `Database` object.
// +enum
type UsageSpecType string

const (
	// GrantUsageSpecType indicates a grant usage permission.
	// The default usage permission is grant.
	GrantUsageSpecType UsageSpecType = "grant"

	// RevokeUsageSpecType indicates a revoke usage permission.
	RevokeUsageSpecType UsageSpecType = "revoke"
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

	// The list of schemas to be managed in the database
	// +optional
	Schemas []SchemaSpec `json:"schemas,omitempty"`

	// The list of extensions to be managed in the database
	// +optional
	Extensions []ExtensionSpec `json:"extensions,omitempty"`

	// The list of foreign data wrappers to be managed in the database
	// +optional
	FDWs []FDWSpec `json:"fdws,omitempty"`

	// The list of foreign servers to be managed in the database
	// +optional
	Servers []ServerSpec `json:"servers,omitempty"`
}

// DatabaseObjectSpec contains the fields which are common to every
// database object
type DatabaseObjectSpec struct {
	// Name of the object (extension, schema, FDW, server)
	Name string `json:"name"`

	// Specifies whether an object (e.g schema) should be present or absent
	// in the database. If set to `present`, the object will be created if
	// it does not exist. If set to `absent`, the extension/schema will be
	// removed if it exists.
	// +kubebuilder:default:="present"
	// +kubebuilder:validation:Enum=present;absent
	// +optional
	Ensure EnsureOption `json:"ensure"`
}

// SchemaSpec configures a schema in a database
type SchemaSpec struct {
	// Common fields
	DatabaseObjectSpec `json:",inline"`

	// The role name of the user who owns the schema inside PostgreSQL.
	// It maps to the `AUTHORIZATION` parameter of `CREATE SCHEMA` and the
	// `OWNER TO` command of `ALTER SCHEMA`.
	Owner string `json:"owner,omitempty"`

	// List of roles for which `CREATE` privileges on the schema are granted or revoked.
	// Maps to the `GRANT CREATE ON SCHEMA` and `REVOKE CREATE ON SCHEMA` SQL commands.
	// +optional
	Create []UsageSpec `json:"create,omitempty"`

	// List of roles for which `USAGE` privileges on the schema are granted or revoked.
	// Maps to the `GRANT USAGE ON SCHEMA` and `REVOKE USAGE ON SCHEMA` SQL commands.
	// +optional
	Usage []UsageSpec `json:"usage,omitempty"`
}

// ExtensionSpec configures an extension in a database
type ExtensionSpec struct {
	// Common fields
	DatabaseObjectSpec `json:",inline"`

	// The version of the extension to install. If empty, the operator will
	// install the default version (whatever is specified in the
	// extension's control file)
	Version string `json:"version,omitempty"`

	// The name of the schema in which to install the extension's objects,
	// in case the extension allows its contents to be relocated. If not
	// specified (default), and the extension's control file does not
	// specify a schema either, the current default object creation schema
	// is used.
	Schema string `json:"schema,omitempty"`
}

// FDWSpec configures an Foreign Data Wrapper in a database
type FDWSpec struct {
	// Common fields
	DatabaseObjectSpec `json:",inline"`

	// Name of the handler function (e.g., "postgres_fdw_handler").
	// This will be empty if no handler is specified. In that case,
	// the default handler is registered when the FDW extension is created.
	// +optional
	Handler string `json:"handler,omitempty"`

	// Name of the validator function (e.g., "postgres_fdw_validator").
	// This will be empty if no validator is specified. In that case,
	// the default validator is registered when the FDW extension is created.
	// +optional
	Validator string `json:"validator,omitempty"`

	// Owner specifies the database role that will own the Foreign Data Wrapper.
	// The role must have superuser privileges in the target database.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Options specifies the configuration options for the FDW.
	// +optional
	Options []OptionSpec `json:"options,omitempty"`

	// List of roles for which `USAGE` privileges on the FDW are granted or revoked.
	// +optional
	Usages []UsageSpec `json:"usage,omitempty"`
}

// ServerSpec configures a server of a foreign data wrapper
type ServerSpec struct {
	// Common fields
	DatabaseObjectSpec `json:",inline"`

	// The name of the Foreign Data Wrapper (FDW)
	// +kubebuilder:validation:XValidation:rule="self != ''",message="fdw is required"
	FdwName string `json:"fdw"`

	// Options specifies the configuration options for the server
	// (key is the option name, value is the option value).
	// +optional
	Options []OptionSpec `json:"options,omitempty"`

	// List of roles for which `USAGE` privileges on the server are granted or revoked.
	// +optional
	Usages []UsageSpec `json:"usage,omitempty"`
}

// OptionSpec holds the name, value and the ensure field for an option
type OptionSpec struct {
	// Name of the option
	Name string `json:"name"`

	// Value of the option
	Value string `json:"value"`

	// Specifies whether an option should be present or absent in
	// the database. If set to `present`, the option will be
	// created if it does not exist. If set to `absent`, the
	// option will be removed if it exists.
	// +kubebuilder:default:="present"
	// +kubebuilder:validation:Enum=present;absent
	// +optional
	Ensure EnsureOption `json:"ensure,omitempty"`
}

// UsageSpec configures a usage for a foreign data wrapper
type UsageSpec struct {
	// Name of the usage
	// +kubebuilder:validation:XValidation:rule="self != ''",message="name is required"
	Name string `json:"name"`

	// The type of usage
	// +kubebuilder:default:="grant"
	// +kubebuilder:validation:Enum=grant;revoke
	// +optional
	Type UsageSpecType `json:"type,omitempty"`
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

	// Schemas is the status of the managed schemas
	// +optional
	Schemas []DatabaseObjectStatus `json:"schemas,omitempty"`

	// Extensions is the status of the managed extensions
	// +optional
	Extensions []DatabaseObjectStatus `json:"extensions,omitempty"`

	// FDWs is the status of the managed FDWs
	// +optional
	FDWs []DatabaseObjectStatus `json:"fdws,omitempty"`

	// Servers is the status of the managed servers
	// +optional
	Servers []DatabaseObjectStatus `json:"servers,omitempty"`
}

// DatabaseObjectStatus is the status of the managed database objects
type DatabaseObjectStatus struct {
	// The name of the object
	Name string `json:"name"`

	// True of the object has been installed successfully in
	// the database
	Applied bool `json:"applied"`

	// Message is the object reconciliation message
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
