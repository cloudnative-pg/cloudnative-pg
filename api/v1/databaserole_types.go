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

// DatabaseRoleReclaimPolicy describes a policy for end-of-life maintenance of Roles.
// +enum
type DatabaseRoleReclaimPolicy string

const (
	// DatabaseRoleReclaimDelete means the Role will be deleted from Kubernetes on release
	// from its claim.
	DatabaseRoleReclaimDelete DatabaseRoleReclaimPolicy = "delete"

	// DatabaseRoleReclaimRetain means the Role will be left in its current phase for manual
	// reclamation by the administrator. The default policy is Retain.
	DatabaseRoleReclaimRetain DatabaseRoleReclaimPolicy = "retain"
)

// DatabaseRoleConditionType defines types of role conditions
type DatabaseRoleConditionType string

const (
	// ConditionPasswordSecretChange is true when the operator detects a change
	// in the password Secret referenced by the DatabaseRole.
	ConditionPasswordSecretChange DatabaseRoleConditionType = "PasswordSecretChange"
)

const (
	// clientCertSecretSuffix is the suffix appended to a DatabaseRole name to form
	// the name of the Secret holding its generated TLS client certificate.
	clientCertSecretSuffix = "-client-cert"

	// passwordSecretSuffix is the suffix appended to a DatabaseRole name to form
	// the default name of the Secret holding its generated password.
	passwordSecretSuffix = "-password"
)

// DatabaseRoleSpec represents a role in Postgres
// +kubebuilder:validation:XValidation:rule="self.name == oldSelf.name",message="name is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.ensure) || self.ensure != 'absent'",message="ensure: absent is not supported for DatabaseRole; delete the resource with databaseRoleReclaimPolicy: delete instead"
// +kubebuilder:validation:XValidation:rule="self.name != 'postgres'",message="the role name postgres is reserved"
// +kubebuilder:validation:XValidation:rule="self.name != 'streaming_replica'",message="the role name streaming_replica is reserved"
// +kubebuilder:validation:XValidation:rule="!self.name.startsWith('pg_')",message="role names starting with pg_ are reserved by PostgreSQL"
// +kubebuilder:validation:XValidation:rule="!self.name.startsWith('cnpg_')",message="role names starting with cnpg_ are reserved by the operator"
// +kubebuilder:validation:XValidation:rule="self.name.size() != 0",message="role name must not be empty"
// +kubebuilder:validation:XValidation:rule="!has(self.passwordSecret) || !has(self.disablePassword) || !self.disablePassword",message="passwordSecret and disablePassword are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!has(self.clientCertificate) || !self.clientCertificate.enabled || self.login",message="clientCertificate requires the role to have login enabled"
// +kubebuilder:validation:XValidation:rule="!has(self.password) || !has(self.passwordSecret)",message="password and passwordSecret are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!has(self.password) || !has(self.disablePassword) || !self.disablePassword",message="password and disablePassword are mutually exclusive"
type DatabaseRoleSpec struct {
	// The Kubernetes representation of a PostgreSQL role
	// in the `cluster.spec.managed.roles` definition.
	RoleConfiguration `json:",inline"`

	// The corresponding cluster
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="cluster reference is immutable after creation"
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The policy for end-of-life maintenance of this role
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy DatabaseRoleReclaimPolicy `json:"databaseRoleReclaimPolicy,omitempty"`

	// ClientCertificate configures the operator to generate and renew a TLS client
	// certificate for this role, signed by the cluster's client CA. The certificate
	// is stored in a Secret named `<databaserole-name>-client-cert`.
	// Requires login to be true.
	// +optional
	ClientCertificate *ClientCertificateConfiguration `json:"clientCertificate,omitempty"`

	// Password configures how the operator manages the password of this role,
	// instead of requiring a pre-existing Secret through `passwordSecret` or
	// disabling it through `disablePassword`. Mutually exclusive with both.
	// +optional
	Password *PasswordConfiguration `json:"password,omitempty"`
}

// PasswordMode governs how the operator manages the password of a DatabaseRole.
// +enum
type PasswordMode string

const (
	// PasswordModeGenerate makes the operator generate the password of the role
	// and keep it in a Secret it owns, rotating it when a lifetime is requested.
	PasswordModeGenerate PasswordMode = "generate"

	// PasswordModeSecret makes the operator read the password of the role from
	// an existing Secret named by `secret`, instead of generating one: the
	// Secret is not owned by the role, and the operator never writes to it.
	PasswordModeSecret PasswordMode = "secret"

	// PasswordModeExternal stops the operator from managing the password:
	// any Secret it previously generated is deleted, and the password already
	// set on the role in PostgreSQL is left untouched, as if managed by
	// something else.
	PasswordModeExternal PasswordMode = "external"

	// PasswordModeClear makes the operator set the password of the role to
	// NULL in PostgreSQL, disabling password authentication for it.
	PasswordModeClear PasswordMode = "clear"
)

// PasswordConfiguration configures how the operator manages the password of a
// DatabaseRole.
// +kubebuilder:validation:XValidation:rule="!has(self.renewBefore) || (has(self.duration) && duration(self.renewBefore).getSeconds() * 2 <= duration(self.duration).getSeconds())",message="renewBefore requires duration, and must be at most half of it"
// +kubebuilder:validation:XValidation:rule="!has(self.duration) || duration(self.duration) >= duration('1m')",message="duration must be at least 1m"
// +kubebuilder:validation:XValidation:rule="self.mode != 'secret' || has(self.secret)",message="secret is required when mode is secret"
// +kubebuilder:validation:XValidation:rule="self.mode == 'generate' || self.mode == 'secret' || !has(self.secret)",message="secret is only meaningful when mode is generate or secret"
// +kubebuilder:validation:XValidation:rule="self.mode == 'generate' || !has(self.criteria)",message="criteria is only meaningful when mode is generate"
// +kubebuilder:validation:XValidation:rule="self.mode == 'generate' || (!has(self.duration) && !has(self.renewBefore))",message="duration and renewBefore are only meaningful when mode is generate"
type PasswordConfiguration struct {
	// Mode governs how the operator manages the password of this role, and is
	// required whenever the `password` stanza is present: `generate` generates
	// and rotates a password kept in a Secret the operator owns; `secret`
	// reads the password from an existing Secret named by `secret`, without
	// generating or owning one; `external` stops managing it, deleting any
	// Secret previously generated and leaving the password already set on the
	// role in PostgreSQL untouched; `clear` sets the password of the role to
	// NULL in PostgreSQL, disabling password authentication for it. It has no
	// default: asking for a password without saying how it is managed is
	// ambiguous, and defaulting it either way would silently pick a behavior
	// as consequential as generating a credential or removing one.
	// +kubebuilder:validation:Enum=generate;secret;external;clear
	Mode PasswordMode `json:"mode"`

	// Secret is the name of the Secret holding the password of this role: the
	// one the operator generates into and never overwrites if it does not own
	// (when `mode` is `generate`, defaulting to `<databaserole-name>-password`),
	// or an existing one to read the password from, required and never written
	// to (when `mode` is `secret`). Only meaningful in those two modes.
	// +optional
	Secret string `json:"secret,omitempty"`

	// Duration is the lifetime of the generated password, at least one minute:
	// once it is reached, minus `renewBefore`, the operator generates a new
	// password and applies it to the role. When unset, the password is generated
	// once and never rotated. Only allowed when `mode` is `generate`, since
	// nothing else here rotates a password.
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`

	// RenewBefore is how long before the end of its lifetime the password is
	// rotated. Only meaningful together with `duration`, and it must be at most
	// half of it, so that the password is not due for rotation as soon as it is
	// generated. Defaults to the operator's `EXPIRING_CHECK_THRESHOLD` setting
	// (7 days), capped at half of the lifetime. Only allowed when `mode` is
	// `generate`, since nothing else here rotates a password.
	// +optional
	RenewBefore *metav1.Duration `json:"renewBefore,omitempty"`

	// Criteria constrains the generated password. Only meaningful when `mode`
	// is `generate`.
	// +optional
	Criteria *PasswordCriteria `json:"criteria,omitempty"`
}

// PasswordCriteria describes the shape of a generated password.
//
// Unless `allowRepeat` is set, every character of the password is drawn from a
// different one of the 52 letters, 10 digits and (by default) 30 symbols the
// generator knows: criteria asking for more than are available can never be
// satisfied, and are rejected here rather than failing at generation time.
// +kubebuilder:validation:XValidation:rule="(has(self.digits) ? self.digits : (self.length / 4 > 10 ? 10 : self.length / 4)) + (has(self.symbols) ? self.symbols : 0) <= self.length",message="the number of digits and symbols must not exceed the password length"
// +kubebuilder:validation:XValidation:rule="(has(self.allowRepeat) && self.allowRepeat) || self.length - (has(self.digits) ? self.digits : (self.length / 4 > 10 ? 10 : self.length / 4)) - (has(self.symbols) ? self.symbols : 0) <= ((has(self.noUpper) && self.noUpper) ? 26 : 52)",message="without allowRepeat the password cannot contain more letters than are available: 52, or 26 with noUpper. Shorten the length, or ask for more digits and symbols"
// +kubebuilder:validation:XValidation:rule="(has(self.allowRepeat) && self.allowRepeat) || !has(self.digits) || self.digits <= 10",message="without allowRepeat the password cannot contain more than 10 digits"
// +kubebuilder:validation:XValidation:rule="(has(self.allowRepeat) && self.allowRepeat) || !has(self.symbols) || self.symbols <= (has(self.symbolCharacters) ? size(self.symbolCharacters) : 30)",message="without allowRepeat the password cannot contain more symbols than the distinct characters of symbolCharacters (30 by default)"
type PasswordCriteria struct {
	// Length of the generated password.
	// +kubebuilder:default:=24
	// +kubebuilder:validation:Minimum=8
	// +kubebuilder:validation:Maximum=1024
	// +optional
	Length int `json:"length,omitempty"`

	// Digits is the number of digits in the generated password. Defaults to 25%
	// of its length, and never to more than 10: unless `allowRepeat` is set, the
	// generator cannot use the same digit twice.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Digits *int `json:"digits,omitempty"`

	// Symbols is the number of symbol characters in the generated password.
	// Defaults to 0.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Symbols *int `json:"symbols,omitempty"`

	// SymbolCharacters is the set of symbols the generated password can draw
	// from. Defaults to the symbols of the generator (``~!@#$%^&*()_+-={}|[]\:"<>?,./``).
	// Only ASCII punctuation is accepted: a letter or a digit here would collide
	// with the rest of the password when `allowRepeat` is not set, and whitespace
	// would be trimmed away before the password is applied to the role.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[\x21-\x2F\x3A-\x40\x5B-\x60\x7B-\x7E]+$`
	// +optional
	SymbolCharacters *string `json:"symbolCharacters,omitempty"`

	// NoUpper disables uppercase characters in the generated password.
	// +optional
	NoUpper bool `json:"noUpper,omitempty"`

	// AllowRepeat allows the same character to appear more than once in the
	// generated password.
	// +optional
	AllowRepeat bool `json:"allowRepeat,omitempty"`
}

// GeneratedPasswordState holds the observed state of the generated password.
type GeneratedPasswordState struct {
	// SecretName is the name of the Secret the password was generated into. The
	// operator records it to recognize the Secret as its own once the role stops
	// generating a password, or starts generating it somewhere else, and delete
	// what it left behind.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// IssuedAt is the time the current generated password was issued, in RFC3339
	// format. The expiration and the rotation deadline are derived from it and the
	// role's current duration/renewBefore, so that a change to either takes effect
	// on the next reconciliation instead of being trumped by a deadline computed
	// under the previous settings.
	// +optional
	IssuedAt string `json:"issuedAt,omitempty"`

	// Expiration is the time at which the generated password is considered
	// expired, in RFC3339 format: the operator rotates it `renewBefore` ahead of
	// that. It is empty when rotation is not enabled.
	// +optional
	Expiration string `json:"expiration,omitempty"`

	// Message contains a human-readable explanation of the current password
	// status, such as why generation was skipped or why an existing Secret was
	// left untouched.
	// +optional
	Message string `json:"message,omitempty"`
}

// ClientCertificateConfiguration configures operator-managed issuance of a TLS
// client certificate for a DatabaseRole.
type ClientCertificateConfiguration struct {
	// Enabled turns on client certificate issuance for this role. When true,
	// the role must have login enabled. Defaults to true when the block is present.
	// +kubebuilder:default:=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// ClientCertificateState holds the observed state of the generated TLS client certificate.
type ClientCertificateState struct {
	// Expiration is the expiration time of the generated client certificate, in RFC3339 format.
	// +optional
	Expiration string `json:"expiration,omitempty"`

	// Message contains a human-readable explanation of the current certificate status,
	// such as why issuance was skipped or why an existing Secret was left untouched.
	// +optional
	Message string `json:"message,omitempty"`
}

// DatabaseRoleStatus defines the observed state of a DatabaseRole
type DatabaseRoleStatus struct {
	// A sequence number representing the latest
	// desired state that was synchronized
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Applied is true if the role was reconciled correctly
	// +optional
	Applied *bool `json:"applied,omitempty"`

	// Message is the reconciliation error message
	// +optional
	Message string `json:"message,omitempty"`

	// SecretResourceVersion is the resource version of the password secret
	// last applied to the role; a change to it triggers reconciliation.
	// +optional
	SecretResourceVersion string `json:"secretResourceVersion,omitempty"`

	// ClientCertificate holds the observed state of the generated TLS client
	// certificate, when client certificate issuance is enabled.
	// +optional
	ClientCertificate *ClientCertificateState `json:"clientCertificate,omitempty"`

	// Password holds the observed state of the generated password, when password
	// generation is enabled.
	// +optional
	Password *GeneratedPasswordState `json:"password,omitempty"`

	// Conditions for the DatabaseRole object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
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

// DatabaseRole is the Schema for the databaseroles API
type DatabaseRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the desired DatabaseRole.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec DatabaseRoleSpec `json:"spec"`
	// Most recently observed status of the DatabaseRole. This data may not be up
	// to date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status DatabaseRoleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseRoleList contains a list of DatabaseRoles
type DatabaseRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseRole `json:"items"`
}
