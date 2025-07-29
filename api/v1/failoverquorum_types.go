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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// FailoverQuorumList contains a list of FailoverQuorum
type FailoverQuorumList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of failoverquorums
	Items []FailoverQuorum `json:"items"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// FailoverQuorum contains the information about the current failover
// quorum status of a PG cluster. It is updated by the instance manager
// of the primary node and reset to zero by the operator to trigger
// an update.
type FailoverQuorum struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Most recently observed status of the failover quorum.
	// +optional
	Status FailoverQuorumStatus `json:"status"`
}

// FailoverQuorumStatus is the latest observed status of the failover
// quorum of the PG cluster.
type FailoverQuorumStatus struct {
	// Contains the latest reported Method value.
	// +optional
	Method string `json:"method,omitempty"`

	// StandbyNames is the list of potentially synchronous
	// instance names.
	// +optional
	StandbyNames []string `json:"standbyNames,omitempty"`

	// StandbyNumber is the number of synchronous standbys that transactions
	// need to wait for replies from.
	// +optional
	StandbyNumber int `json:"standbyNumber,omitempty"`

	// Primary is the name of the primary instance that updated
	// this object the latest time.
	// +optional
	Primary string `json:"primary,omitempty"`
}

func init() {
	SchemeBuilder.Register(&FailoverQuorum{}, &FailoverQuorumList{})
}
