/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
	// The credentials to use to upload data to S3
	S3Credentials S3Credentials `json:"s3Credentials"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	EndpointURL string `json:"endpointURL,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data
	DestinationPath string `json:"destinationPath"`

	// The server name on S3, the cluster name is used if this
	// parameter is omitted
	ServerName string `json:"serverName,omitempty"`

	// Encryption method required to S3 API
	Encryption string `json:"encryption,omitempty"`

	// The ID of the Barman backup
	BackupID string `json:"backupId,omitempty"`

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
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.error"

// Backup is the Schema for the backups API
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of the backup.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec BackupSpec `json:"spec,omitempty"`
	// Most recently observed status of the backup. This data may not be up to
	// date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Status BackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of backups
	Items []Backup `json:"items"`
}

// SetAsFailed marks a certain backup as invalid
func (backupStatus *BackupStatus) SetAsFailed(
	stdout string,
	stderr string,
	err error,
) {
	backupStatus.Phase = BackupPhaseFailed
	backupStatus.CommandOutput = stdout
	backupStatus.CommandError = stderr

	if err != nil {
		backupStatus.Error = err.Error()
	} else {
		backupStatus.Error = ""
	}
}

// SetAsCompleted marks a certain backup as invalid
func (backupStatus *BackupStatus) SetAsCompleted(
	stdout string,
	stderr string,
) {
	backupStatus.Phase = BackupPhaseCompleted
	backupStatus.CommandOutput = stdout
	backupStatus.CommandError = stderr
	backupStatus.Error = ""
}

// IsDone check if a backup is completed or still in progress
func (backupStatus *BackupStatus) IsDone() bool {
	return backupStatus.Phase == BackupPhaseCompleted || backupStatus.Phase == BackupPhaseFailed
}

// IsInProgress check if a certain backup is in progress or not
func (backupStatus *BackupStatus) IsInProgress() bool {
	return !backupStatus.IsDone()
}

// GetStatus gets the backup status
func (backup *Backup) GetStatus() *BackupStatus {
	return &backup.Status
}

// GetName get the backup name
func (backup *Backup) GetName() string {
	return backup.Name
}

// GetNamespace get the backup namespace
func (backup *Backup) GetNamespace() string {
	return backup.Namespace
}

// GetKubernetesObject get the kubernetes object
func (backup *Backup) GetKubernetesObject() client.Object {
	return backup
}

// Hub marks this type as a conversion hub.
func (*Backup) Hub() {}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
