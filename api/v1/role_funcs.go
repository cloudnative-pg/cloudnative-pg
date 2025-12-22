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
	"k8s.io/utils/ptr"
)

// SetAsFailed sets the publication as failed with the given error
func (r *Role) SetAsFailed(err error) {
	r.Status.Applied = ptr.To(false)
	r.Status.Message = err.Error()
}

// SetAsApplied sets the subscription as working correctly
func (r *Role) SetAsApplied() {
	r.Status.Message = ""
	r.Status.Applied = ptr.To(true)
	r.Status.ObservedGeneration = r.Generation
}

// GetRoleInherit returns the inherit attribute of a roleConfiguration
func (roleSpec *RoleSpec) GetRoleInherit() bool {
	if roleSpec.Inherit != nil {
		return *roleSpec.Inherit
	}
	return true
}

// GetRoleSecretName gets the name of the secret holding the role password
func (roleSpec *RoleSpec) GetRoleSecretName() string {
	if roleSpec.PasswordSecret == nil {
		return ""
	}
	return roleSpec.PasswordSecret.Name
}

// GetRoleName gets the role name
func (roleSpec *RoleSpec) GetRoleName() string {
	return roleSpec.Name
}

// ShouldDisablePassword checks if the password should be disabled in Postgres
func (roleSpec *RoleSpec) ShouldDisablePassword() bool {
	return roleSpec.DisablePassword
}

// HasReconciliations returns true if the role has been reconciled at least once
func (r *Role) HasReconciliations() bool {
	return r.Status.ObservedGeneration > 0
}

// MustHaveManagedResourceExclusivity detects conflicting roles
func (roleList *RoleList) MustHaveManagedResourceExclusivity(role *Role) error {
	pointers := toSliceWithPointers(roleList.Items)
	return ensureManagedResourceExclusivity(role, pointers)
}

// GetClusterRef returns the cluster reference of the role
func (r *Role) GetClusterRef() corev1.LocalObjectReference {
	return r.Spec.ClusterRef
}

// GetManagedObjectName returns the name of the managed role object
func (r *Role) GetManagedObjectName() string {
	return r.Spec.Name
}

// GetStatusMessage returns the status message of the role
func (r *Role) GetStatusMessage() string {
	return r.Status.Message
}

// SetStatusObservedGeneration sets the observed generation of the role
func (r *Role) SetStatusObservedGeneration(obsGeneration int64) {
	r.Status.ObservedGeneration = obsGeneration
}
