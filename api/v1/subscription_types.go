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

// SubscriptionReclaimPolicy describes a policy for end-of-life maintenance of Subscriptions.
// +enum
type SubscriptionReclaimPolicy string

const (
	// SubscriptionReclaimDelete means the subscription will be deleted from Kubernetes on release
	// from its claim.
	SubscriptionReclaimDelete SubscriptionReclaimPolicy = "delete"

	// SubscriptionReclaimRetain means the subscription will be left in its current phase for manual
	// reclamation by the administrator. The default policy is Retain.
	SubscriptionReclaimRetain SubscriptionReclaimPolicy = "retain"
)

// SubscriptionSpec defines the desired state of Subscription
type SubscriptionSpec struct {
	// The name of the PostgreSQL cluster that identifies the "subscriber"
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The name of the subscription inside PostgreSQL
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name"`

	// The name of the database where the publication will be installed in
	// the "subscriber" cluster
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="dbname is immutable"
	DBName string `json:"dbname"`

	// Subscription parameters included in the `WITH` clause of the PostgreSQL
	// `CREATE SUBSCRIPTION` command. Most parameters cannot be changed
	// after the subscription is created and will be ignored if modified
	// later, except for a limited set documented at:
	// https://www.postgresql.org/docs/current/sql-altersubscription.html#SQL-ALTERSUBSCRIPTION-PARAMS-SET
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// The name of the publication inside the PostgreSQL database in the
	// "publisher"
	PublicationName string `json:"publicationName"`

	// The name of the database containing the publication on the external
	// cluster. Defaults to the one in the external cluster definition.
	// +optional
	PublicationDBName string `json:"publicationDBName,omitempty"`

	// The name of the external cluster with the publication ("publisher")
	ExternalClusterName string `json:"externalClusterName"`

	// The policy for end-of-life maintenance of this subscription
	// +kubebuilder:validation:Enum=delete;retain
	// +kubebuilder:default:=retain
	// +optional
	ReclaimPolicy SubscriptionReclaimPolicy `json:"subscriptionReclaimPolicy,omitempty"`
}

// SubscriptionStatus defines the observed state of Subscription
type SubscriptionStatus struct {
	// A sequence number representing the latest
	// desired state that was synchronized
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Applied is true if the subscription was reconciled correctly
	// +optional
	Applied *bool `json:"applied,omitempty"`

	// Message is the reconciliation output message
	// +optional
	Message string `json:"message,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Applied",type="boolean",JSONPath=".status.applied"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Latest reconciliation message"

// Subscription is the Schema for the subscriptions API
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   SubscriptionSpec   `json:"spec"`
	Status SubscriptionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Subscription{}, &SubscriptionList{})
}
