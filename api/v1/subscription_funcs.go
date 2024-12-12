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

// SetAsFailed sets the subscription as failed with the given error
func (sub *Subscription) SetAsFailed(err error) {
	sub.Status.Applied = ptr.To(false)
	sub.Status.Message = err.Error()
}

// SetAsUnknown sets the subscription as unknown with the given error
func (sub *Subscription) SetAsUnknown(err error) {
	sub.Status.Applied = nil
	sub.Status.Message = err.Error()
}

// SetAsReady sets the subscription as working correctly
func (sub *Subscription) SetAsReady() {
	sub.Status.Applied = ptr.To(true)
	sub.Status.Message = ""
	sub.Status.ObservedGeneration = sub.Generation
}

// GetStatusMessage returns the status message of the subscription
func (sub *Subscription) GetStatusMessage() string {
	return sub.Status.Message
}

// GetClusterRef returns the cluster reference of the subscription
func (sub *Subscription) GetClusterRef() corev1.LocalObjectReference {
	return sub.Spec.ClusterRef
}

// GetName returns the subscription object name
func (sub *Subscription) GetName() string {
	return sub.Name
}

// GetManagedObjectName returns the name of the managed subscription object
func (sub *Subscription) GetManagedObjectName() string {
	return sub.Spec.Name
}

// HasReconciliations returns true if the subscription has been reconciled at least once
func (sub *Subscription) HasReconciliations() bool {
	return sub.Status.ObservedGeneration > 0
}

// MustHaveManagedResourceExclusivity detects conflicting subscriptions
func (pub *SubscriptionList) MustHaveManagedResourceExclusivity(reference *Subscription) error {
	pointers := toSliceWithPointers(pub.Items)
	return ensureManagedResourceExclusivity(reference, pointers)
}
