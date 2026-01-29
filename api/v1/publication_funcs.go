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
	"k8s.io/utils/ptr"
)

// SetAsFailed sets the publication as failed with the given error
func (pub *Publication) SetAsFailed(err error) {
	pub.Status.Applied = ptr.To(false)
	pub.Status.Message = err.Error()
}

// SetAsUnknown sets the publication as unknown with the given error
func (pub *Publication) SetAsUnknown(err error) {
	pub.Status.Applied = nil
	pub.Status.Message = err.Error()
}

// SetAsReady sets the subscription as working correctly
func (pub *Publication) SetAsReady() {
	pub.Status.Applied = ptr.To(true)
	pub.Status.Message = ""
	pub.Status.ObservedGeneration = pub.Generation
}

// GetStatusMessage returns the status message of the publication
func (pub *Publication) GetStatusMessage() string {
	return pub.Status.Message
}

// GetClusterRef returns the cluster reference of the publication
func (pub *Publication) GetClusterRef() ClusterObjectReference {
	return ClusterObjectReference{
		Name: pub.Spec.ClusterRef.Name,
	}
}

// GetClusterNamespace returns the namespace of the referenced cluster.
// Publications do not support cross-namespace references, so this always
// returns the Publication's namespace.
func (pub *Publication) GetClusterNamespace() string {
	return pub.Namespace
}

// GetManagedObjectName returns the name of the managed publication object
func (pub *Publication) GetManagedObjectName() string {
	return pub.Spec.Name
}

// HasReconciliations returns true if the publication has been reconciled at least once
func (pub *Publication) HasReconciliations() bool {
	return pub.Status.ObservedGeneration > 0
}

// GetName returns the publication name
func (pub *Publication) GetName() string {
	return pub.Name
}

// SetStatusObservedGeneration sets the observed generation of the publication
func (pub *Publication) SetStatusObservedGeneration(obsGeneration int64) {
	pub.Status.ObservedGeneration = obsGeneration
}

// MustHaveManagedResourceExclusivity detects conflicting publications
func (pub *PublicationList) MustHaveManagedResourceExclusivity(reference *Publication) error {
	pointers := toSliceWithPointers(pub.Items)
	return ensureManagedResourceExclusivity(reference, pointers)
}
