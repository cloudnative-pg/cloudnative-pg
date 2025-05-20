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

// +kubebuilder:object:root=true

// SyncQuorumList contains a list of SyncQuorum
type SyncQuorumList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of syncquorums
	Items []SyncQuorum `json:"items"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"

// SyncQuorum contains the information about the current synchronous
// quorum status of a PG cluster. It is updated by the instance manager
// of the primary node and reset to zero by the operator to trigger
// an update.
type SyncQuorum struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the cluster that will refresh this data
	Spec SyncQuorumSpec `json:"spec"`

	// Most recently observed status of the sync quorum.
	// +optional
	Status SyncQuorumStatus `json:"status"`
}

// SyncQuorumSpec contains the pointer to the cluster that should keep
// the status updated.
type SyncQuorumSpec struct {
	// The name of the PostgreSQL cluster hosting the database.
	ClusterRef corev1.LocalObjectReference `json:"cluster"`
}

// SyncQuorumStatus is the latest observed status of the synchronous
// quorum of the PG cluster.
type SyncQuorumStatus struct {
	// Contains the latest reported Method value.
	// +optional
	Method string `json:"method,omitempty"`

	// StandbyNames is the list of potentially synchronous
	// instance names
	// +optional
	StandbyNames []string `json:"standbyNames,omitempty"`

	// StandbyNumber is the quorum of instances that will be
	// synchronous, to be chosen within SynchronousStandbyNamesList
	// +optional
	StandbyNumber int `json:"standbyNumber,omitempty"`

	// Primary is the name of the primary instance that updated
	// this object the latest time.
	// +optional
	Primary string `json:"primary,omitempty"`
}

func init() {
	SchemeBuilder.Register(&SyncQuorum{}, &SyncQuorumList{})
}
