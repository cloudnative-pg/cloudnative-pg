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
// +kubebuilder:validation:XValidation:rule="!has(self.issueClientCertificate) || !self.issueClientCertificate || self.login",message="issueClientCertificate requires the role to have login enabled"
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

	// IssueClientCertificate enables the operator to generate and renew a TLS client
	// certificate for this role, signed by the cluster's client CA. The certificate
	// is stored in a Secret named `<databaserole-name>-client-cert`.
	// Requires login to be true.
	// +optional
	IssueClientCertificate bool `json:"issueClientCertificate,omitempty"`
}

// ClientCertificateState holds the observed state of the generated TLS client certificate.
type ClientCertificateState struct {
	// Expiration is the expiration time of the generated client certificate, in RFC3339 format.
	// +optional
	Expiration string `json:"expiration,omitempty"`

	// Message contains a human-readable explanation of the current certificate status,
	// such as why issuance was skipped.
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
	// certificate, when issueClientCertificate is true.
	// +optional
	ClientCertificate *ClientCertificateState `json:"clientCertificate,omitempty"`

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
