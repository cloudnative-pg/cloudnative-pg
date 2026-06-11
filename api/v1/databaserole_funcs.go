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
	"k8s.io/utils/ptr"
)

// SetAsFailed sets the role as failed with the given error
func (r *DatabaseRole) SetAsFailed(err error) {
	r.Status.Applied = ptr.To(false)
	r.Status.Message = err.Error()
}

// SetAsReady sets the role as working correctly
func (r *DatabaseRole) SetAsReady() {
	r.Status.Message = ""
	r.Status.Applied = ptr.To(true)
	r.Status.ObservedGeneration = r.Generation
}

// SetAsUnknown sets the role's applied state as unknown with the given error
func (r *DatabaseRole) SetAsUnknown(err error) {
	r.Status.Applied = nil
	r.Status.Message = err.Error()
}

// HasReconciliations returns true if the role has been reconciled at least once
func (r *DatabaseRole) HasReconciliations() bool {
	return r.Status.ObservedGeneration > 0
}

// MustHaveManagedResourceExclusivity detects conflicting roles
func (roleList *DatabaseRoleList) MustHaveManagedResourceExclusivity(role *DatabaseRole) error {
	pointers := toSliceWithPointers(roleList.Items)
	return ensureManagedResourceExclusivity(role, pointers)
}

// GetClusterRef returns the cluster reference of the role
func (r *DatabaseRole) GetClusterRef() corev1.LocalObjectReference {
	return r.Spec.ClusterRef
}

// GetManagedObjectName returns the name of the managed role object
func (r *DatabaseRole) GetManagedObjectName() string {
	return r.Spec.Name
}

// GetStatusMessage returns the status message of the role
func (r *DatabaseRole) GetStatusMessage() string {
	return r.Status.Message
}

// SetStatusObservedGeneration sets the observed generation of the role
func (r *DatabaseRole) SetStatusObservedGeneration(obsGeneration int64) {
	r.Status.ObservedGeneration = obsGeneration
}
