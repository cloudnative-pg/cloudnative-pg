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

// ScheduledBackupSpec defines the desired state of ScheduledBackup
type ScheduledBackupSpec struct {
	// If this backup is suspended or not
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// If the first backup has to be immediately start after creation or not
	// +optional
	Immediate *bool `json:"immediate,omitempty"`

	// The schedule does not follow the same format used in Kubernetes CronJobs
	// as it includes an additional seconds specifier,
	// see https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format
	Schedule string `json:"schedule"`

	// The cluster to backup
	Cluster LocalObjectReference `json:"cluster"`

	// Indicates which ownerReference should be put inside the created backup resources.<br />
	// - none: no owner reference for created backup objects (same behavior as before the field was introduced)<br />
	// - self: sets the Scheduled backup object as owner of the backup<br />
	// - cluster: set the cluster as owner of the backup<br />
	// +kubebuilder:validation:Enum=none;self;cluster
	// +kubebuilder:default:=none
	// +optional
	BackupOwnerReference string `json:"backupOwnerReference,omitempty"`

	// The policy to decide which instance should perform this backup. If empty,
	// it defaults to `cluster.spec.backup.target`.
	// Available options are empty string, `primary` and `prefer-standby`.
	// `primary` to have backups run always on primary instances,
	// `prefer-standby` to have backups run preferably on the most updated
	// standby, if available.
	// +kubebuilder:validation:Enum=primary;prefer-standby
	// +optional
	Target BackupTarget `json:"target,omitempty"`

	// The backup method to be used, possible options are `barmanObjectStore`,
	// `volumeSnapshot` or `plugin`. Defaults to: `barmanObjectStore`.
	// +optional
	// +kubebuilder:validation:Enum=barmanObjectStore;volumeSnapshot;plugin
	// +kubebuilder:default:=barmanObjectStore
	Method BackupMethod `json:"method,omitempty"`

	// Configuration parameters passed to the plugin managing this backup
	// +optional
	PluginConfiguration *BackupPluginConfiguration `json:"pluginConfiguration,omitempty"`

	// Whether the default type of backup with volume snapshots is
	// online/hot (`true`, default) or offline/cold (`false`)
	// Overrides the default setting specified in the cluster field '.spec.backup.volumeSnapshot.online'
	// +optional
	Online *bool `json:"online,omitempty"`

	// Configuration parameters to control the online/hot backup with volume snapshots
	// Overrides the default settings specified in the cluster '.backup.volumeSnapshot.onlineConfiguration' stanza
	// +optional
	OnlineConfiguration *OnlineConfiguration `json:"onlineConfiguration,omitempty"`
}

// ScheduledBackupStatus defines the observed state of ScheduledBackup
type ScheduledBackupStatus struct {
	// The latest time the schedule
	// +optional
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// Information when was the last time that backup was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// Next time we will run a backup
	// +optional
	NextScheduleTime *metav1.Time `json:"nextScheduleTime,omitempty"`

	// Error is the latest admission validation error
	// +optional
	Error string `json:"error,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="Last Backup",type="date",JSONPath=".status.lastScheduleTime"

// ScheduledBackup is the Schema for the scheduledbackups API
type ScheduledBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the desired behavior of the ScheduledBackup.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec ScheduledBackupSpec `json:"spec"`
	// Most recently observed status of the ScheduledBackup. This data may not be up
	// to date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status ScheduledBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScheduledBackupList contains a list of ScheduledBackup
type ScheduledBackupList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of clusters
	Items []ScheduledBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScheduledBackup{}, &ScheduledBackupList{})
}
