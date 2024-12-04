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

// GetStatusMessage returns the applied status of the publication
func (pub *Publication) GetStatusMessage() string {
	return pub.Status.Message
}

// GetClusterRef returns the cluster reference of the publication
func (pub *Publication) GetClusterRef() corev1.LocalObjectReference {
	return pub.Spec.ClusterRef
}
