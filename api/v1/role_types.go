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

// RoleSpec represents a role in Postgres
type RoleSpec struct {
	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`
	// The policy for end-of-life maintenance of this role
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy RoleReclaimPolicy `json:"roleReclaimPolicy,omitempty"`

	// Name of the role
	Name string `json:"name"`
	// Description of the role
	// +optional
	Comment string `json:"comment,omitempty"`

	// Ensure the role is `present` or `absent` - defaults to "present"
	// +kubebuilder:default:="present"
	// +kubebuilder:validation:Enum=present;absent
	// +optional
	Ensure EnsureOption `json:"ensure,omitempty"`

	// Secret containing the password of the role (if present)
	// If null, the password will be ignored unless DisablePassword is set
	// +optional
	PasswordSecret *LocalObjectReference `json:"passwordSecret,omitempty"`

	// If the role can log in, this specifies how many concurrent
	// connections the role can make. `-1` (the default) means no limit.
	// +kubebuilder:default:=-1
	// +optional
	ConnectionLimit int64 `json:"connectionLimit,omitempty"`

	// Date and time after which the role's password is no longer valid.
	// When omitted, the password will never expire (default).
	// +optional
	ValidUntil *metav1.Time `json:"validUntil,omitempty"`

	// List of one or more existing roles to which this role will be
	// immediately added as a new member. Default empty.
	// +optional
	InRoles []string `json:"inRoles,omitempty"`

	// Whether a role "inherits" the privileges of roles it is a member of.
	// Defaults is `true`.
	// +kubebuilder:default:=true
	// +optional
	Inherit *bool `json:"inherit,omitempty"` // IMPORTANT default is INHERIT

	// DisablePassword indicates that a role's password should be set to NULL in Postgres
	// +optional
	DisablePassword bool `json:"disablePassword,omitempty"`

	// Whether the role is a `superuser` who can override all access
	// restrictions within the database - superuser status is dangerous and
	// should be used only when really needed. You must yourself be a
	// superuser to create a new superuser. Defaults is `false`.
	// +optional
	Superuser bool `json:"superuser,omitempty"`

	// When set to `true`, the role being defined will be allowed to create
	// new databases. Specifying `false` (default) will deny a role the
	// ability to create databases.
	// +optional
	CreateDB bool `json:"createdb,omitempty"`

	// Whether the role will be permitted to create, alter, drop, comment
	// on, change the security label for, and grant or revoke membership in
	// other roles. Default is `false`.
	// +optional
	CreateRole bool `json:"createrole,omitempty"`

	// Whether the role is allowed to log in. A role having the `login`
	// attribute can be thought of as a user. Roles without this attribute
	// are useful for managing database privileges, but are not users in
	// the usual sense of the word. Default is `false`.
	// +optional
	Login bool `json:"login,omitempty"`

	// Whether a role is a replication role. A role must have this
	// attribute (or be a superuser) in order to be able to connect to the
	// server in replication mode (physical or logical replication) and in
	// order to be able to create or drop replication slots. A role having
	// the `replication` attribute is a very highly privileged role, and
	// should only be used on roles actually used for replication. Default
	// is `false`.
	// +optional
	Replication bool `json:"replication,omitempty"`

	// Whether a role bypasses every row-level security (RLS) policy.
	// Default is `false`.
	// +optional
	BypassRLS bool `json:"bypassrls,omitempty"` // Row-Level Security
}

// RoleState defines the observed state of a Role
// TODO: the existing RoleStatus in the cluster managed roles, does more than we need
type RoleState struct {
	// A sequence number representing the latest
	// desired state that was synchronized
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Applied is true if the role was reconciled correctly
	Applied bool `json:"ready,omitempty"`

	// Message is the reconciliation error message
	Message string `json:"message,omitempty"`
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
