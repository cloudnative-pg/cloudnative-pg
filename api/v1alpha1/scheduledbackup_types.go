/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

// ScheduledBackupSpec defines the desired state of ScheduledBackup
type ScheduledBackupSpec struct {
	// If this backup is suspended of not
	Suspend *bool `json:"suspend,omitempty"`

	// The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.
	Schedule string `json:"schedule"`

	// The cluster to backup
	Cluster v1.LocalObjectReference `json:"cluster,omitempty"`
}

// ScheduledBackupStatus defines the observed state of ScheduledBackup
type ScheduledBackupStatus struct {
	// The latest time the schedule
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// Information when was the last time that backup was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScheduledBackup is the Schema for the scheduledbackups API
type ScheduledBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScheduledBackupSpec   `json:"spec,omitempty"`
	Status ScheduledBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScheduledBackupList contains a list of ScheduledBackup
type ScheduledBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScheduledBackup `json:"items"`
}

// IsSuspended check if a scheduled backup has been suspended or not
func (scheduledBackup ScheduledBackup) IsSuspended() bool {
	if scheduledBackup.Spec.Suspend == nil {
		return false
	}

	return *scheduledBackup.Spec.Suspend
}

// GetName gets the scheduled backup name
func (scheduledBackup *ScheduledBackup) GetName() string {
	return scheduledBackup.Name
}

// GetNamespace gets the scheduled backup name
func (scheduledBackup *ScheduledBackup) GetNamespace() string {
	return scheduledBackup.Namespace
}

// GetSchedule get the cron-like schedule of this scheduled backup
func (scheduledBackup *ScheduledBackup) GetSchedule() string {
	return scheduledBackup.Spec.Schedule
}

// GetStatus gets the status that the caller may update
func (scheduledBackup *ScheduledBackup) GetStatus() *ScheduledBackupStatus {
	return &scheduledBackup.Status
}

// GetKubernetesObject gets the kubernetes object
func (scheduledBackup *ScheduledBackup) GetKubernetesObject() runtime.Object {
	return scheduledBackup
}

// CreateBackup create a backup from this scheduled backup
func (scheduledBackup *ScheduledBackup) CreateBackup(name string) BackupCommon {
	backup := Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scheduledBackup.Namespace,
		},
		Spec: BackupSpec{
			Cluster: scheduledBackup.Spec.Cluster,
		},
	}
	utils.SetAsOwnedBy(&backup.ObjectMeta, scheduledBackup.ObjectMeta, scheduledBackup.TypeMeta)
	return &backup
}

func init() {
	SchemeBuilder.Register(&ScheduledBackup{}, &ScheduledBackupList{})
}
