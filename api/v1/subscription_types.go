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
	// The corresponding cluster
	ClusterRef corev1.LocalObjectReference `json:"cluster"`

	// The name inside PostgreSQL
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name"`

	// The owner
	Owner string `json:"owner,omitempty"`

	// The name of the database
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="dbname is immutable"
	DBName string `json:"dbname"`

	// Parameters
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// The name of the publication
	PublicationName string `json:"publicationName"`

	// The name of the external cluster with the publication
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

	// Ready is true if the database was reconciled correctly
	Ready bool `json:"ready,omitempty"`

	// Error is the reconciliation error message
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="PG Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.error",description="Latest error message"

// Subscription is the Schema for the subscriptions API
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubscriptionSpec   `json:"spec,omitempty"`
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
