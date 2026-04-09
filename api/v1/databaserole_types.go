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

// DatabaseRoleSpec represents a role in Postgres
// +kubebuilder:validation:XValidation:rule="self.name == oldSelf.name",message="name is immutable"
// +kubebuilder:validation:XValidation:rule="self.cluster == oldSelf.cluster",message="cluster is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.ensure) || self.ensure != 'absent'",message="ensure: absent is not supported for DatabaseRole; delete the resource with roleReclaimPolicy: delete instead"
// +kubebuilder:validation:XValidation:rule=”size(self.name) > 0”,message=”name must not be empty”
// +kubebuilder:validation:XValidation:rule="self.name != 'postgres'",message="the role name postgres is reserved"
// +kubebuilder:validation:XValidation:rule="self.name != 'streaming_replica'",message="the role name streaming_replica is reserved"
// +kubebuilder:validation:XValidation:rule="!self.name.startsWith('pg_')",message="role names starting with pg_ are reserved by PostgreSQL"
// +kubebuilder:validation:XValidation:rule="!self.name.startsWith('cnpg_')",message="role names starting with cnpg_ are reserved by the operator"
// +kubebuilder:validation:XValidation:rule="!has(self.passwordSecret) || !self.disablePassword",message="passwordSecret and disablePassword are mutually exclusive"
type DatabaseRoleSpec struct {
	// The Kubernetes representation of a PostgreSQL role
	// in the `cluster.spec.managed.roles` definition.
	RoleConfiguration `json:",inline"`

	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The policy for end-of-life maintenance of this role
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy DatabaseRoleReclaimPolicy `json:"roleReclaimPolicy,omitempty"`
}

// DatabaseRoleState defines the observed state of a DatabaseRole
type DatabaseRoleState struct {
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

	// PasswordState holds the last applied version of the passwordSecret, and
	// the last transaction ID of the role in postgres
	PasswordState PasswordState `json:"passwordState,omitempty"`

	// Conditions for the DatabaseRole object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Applied",type="boolean",JSONPath=".status.applied"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Latest message"

// DatabaseRole is the Schema for the databaseroles API
type DatabaseRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseRoleSpec  `json:"spec,omitempty"`
	Status DatabaseRoleState `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseRoleList contains a list of DatabaseRoles
type DatabaseRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseRole{}, &DatabaseRoleList{})
}
