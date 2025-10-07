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

package status

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectStore represents the Barman ObjectStore CRD from the barman-cloud plugin.
// This is a minimal representation containing only the fields we need for status display.
type ObjectStore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Status contains the observed state of the ObjectStore
	Status ObjectStoreStatus `json:"status,omitempty"`
}

// ObjectStoreStatus defines the observed state of ObjectStore.
type ObjectStoreStatus struct {
	// ServerRecoveryWindow maps each server to its recovery window
	ServerRecoveryWindow map[string]RecoveryWindow `json:"serverRecoveryWindow,omitempty"`
}

// RecoveryWindow represents the time span between the first
// recoverability point and the last successful backup of a PostgreSQL
// server, defining the period during which data can be restored.
type RecoveryWindow struct {
	// The first recoverability point in a PostgreSQL server refers to
	// the earliest point in time to which the database can be
	// restored.
	FirstRecoverabilityPoint *metav1.Time `json:"firstRecoverabilityPoint,omitempty"`

	// The last successful backup time
	LastSuccessfulBackupTime *metav1.Time `json:"lastSuccessfulBackupTime,omitempty"`

	// The last failed backup time
	LastFailedBackupTime *metav1.Time `json:"lastFailedBackupTime,omitempty"`
}
