/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BackupPhase is the phase of the backup
type BackupPhase string

const (
	// BackupPhaseStarted means that the backup is now running
	BackupPhaseStarted = "started"

	// BackupPhaseRunning means that the backup is now running
	BackupPhaseRunning = "running"

	// BackupPhaseCompleted means that the backup is now completed
	BackupPhaseCompleted = "completed"

	// BackupPhaseFailed means that the
	BackupPhaseFailed = "failed"
)

// BackupSpec defines the desired state of Backup
type BackupSpec struct {
	// The cluster to backup
	Cluster v1.LocalObjectReference `json:"cluster,omitempty"`
}

// BackupStatus defines the observed state of Backup
type BackupStatus struct {
	// The last backup status
	Phase BackupPhase `json:"phase,omitempty"`

	// When the backup was started
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// When the backup was terminated
	StoppedAt *metav1.Time `json:"stoppedAt,omitempty"`

	// The detected error
	Error string `json:"error,omitempty"`

	// The backup command output
	CommandOutput string `json:"commandOutput,omitempty"`

	// The backup command output
	CommandError string `json:"commandError,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Backup is the Schema for the backups API
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSpec   `json:"spec,omitempty"`
	Status BackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Backup `json:"items"`
}

// SetAsFailed marks a certain backup as invalid
func (backup *Backup) SetAsFailed(
	stdout string,
	stderr string,
	err error,
) {
	backup.Status.Phase = BackupPhaseFailed
	backup.Status.CommandOutput = stdout
	backup.Status.CommandError = stderr

	if err != nil {
		backup.Status.Error = err.Error()
	} else {
		backup.Status.Error = ""
	}
}

// SetAsCompleted marks a certain backup as invalid
func (backup *Backup) SetAsCompleted(
	stdout string,
	stderr string,
) {
	backup.Status.Phase = BackupPhaseCompleted
	backup.Status.CommandOutput = stdout
	backup.Status.CommandError = stderr
	backup.Status.Error = ""
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
