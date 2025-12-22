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

// RoleReclaimPolicy describes a policy for end-of-life maintenance of Roles.
// +enum
type RoleReclaimPolicy string

const (
	// RoleReclaimDelete means the Role will be deleted from Kubernetes on release
	// from its claim.
	RoleReclaimDelete RoleReclaimPolicy = "delete"

	// RoleReclaimRetain means the Role will be left in its current phase for manual
	// reclamation by the administrator. The default policy is Retain.
	RoleReclaimRetain RoleReclaimPolicy = "retain"
)

// RoleConditionType defines types of role conditions
type RoleConditionType string

const (
	// ConditionPasswordSecretChange is true when the all the instances of the
	// cluster report the same System ID.
	ConditionPasswordSecretChange RoleConditionType = "PasswordSecretChange"
)

// RoleSpec represents a role in Postgres
type RoleSpec struct {
	// The Kubernetes representation of a PostgreSQL role
	// in the `cluster.spec.managed.roles` definition.
	RoleConfiguration `json:",inline"`

	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The policy for end-of-life maintenance of this role
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy RoleReclaimPolicy `json:"roleReclaimPolicy,omitempty"`
}

// RoleState defines the observed state of a Role
// TODO: the existing RoleStatus in the cluster managed roles, does more than we need
type RoleState struct {
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

	// Conditions for cluster object
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

// Role is the Schema for the databases API
type Role struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RoleSpec  `json:"spec,omitempty"`
	Status RoleState `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RoleList contains a list of Roles
type RoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Role `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Role{}, &RoleList{})
}
