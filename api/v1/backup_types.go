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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupPhase is the phase of the backup
type BackupPhase string

const (
	// BackupPhasePending means that the backup is still waiting to be started
	BackupPhasePending = "pending"

	// BackupPhaseStarted means that the backup is now running
	BackupPhaseStarted = "started"

	// BackupPhaseRunning means that the backup is now running
	BackupPhaseRunning = "running"

	// BackupPhaseCompleted means that the backup is now completed
	BackupPhaseCompleted = "completed"

	// BackupPhaseFailed means that the backup is failed
	BackupPhaseFailed = "failed"

	// BackupPhaseWalArchivingFailing means wal archiving isn't properly working
	BackupPhaseWalArchivingFailing = "walArchivingFailing"
)

// BackupSpec defines the desired state of Backup
type BackupSpec struct {
	// The cluster to backup
	Cluster LocalObjectReference `json:"cluster,omitempty"`

	// The policy to decide which instance should perform this backup. If empty,
	// it defaults to `cluster.spec.backup.target`.
	// Available options are empty string, `primary` and `prefer-standby`.
	// `primary` to have backups run always on primary instances,
	// `prefer-standby` to have backups run preferably on the most updated
	// standby, if available.
	// +kubebuilder:validation:Enum=primary;prefer-standby
	Target BackupTarget `json:"target,omitempty"`
}

// BackupStatus defines the observed state of Backup
type BackupStatus struct {
	// The potential credentials for each cloud provider
	BarmanCredentials `json:",inline"`

	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive.
	EndpointCA *SecretKeySelector `json:"endpointCA,omitempty"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	EndpointURL string `json:"endpointURL,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data. This may not be populated in case of errors.
	DestinationPath string `json:"destinationPath,omitempty"`

	// The server name on S3, the cluster name is used if this
	// parameter is omitted
	ServerName string `json:"serverName,omitempty"`

	// Encryption method required to S3 API
	Encryption string `json:"encryption,omitempty"`

	// The ID of the Barman backup
	BackupID string `json:"backupId,omitempty"`

	// The Name of the Barman backup
	BackupName string `json:"backupName,omitempty"`

	// The last backup status
	Phase BackupPhase `json:"phase,omitempty"`

	// When the backup was started
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// When the backup was terminated
	StoppedAt *metav1.Time `json:"stoppedAt,omitempty"`

	// The starting WAL
	BeginWal string `json:"beginWal,omitempty"`

	// The ending WAL
	EndWal string `json:"endWal,omitempty"`

	// The starting xlog
	BeginLSN string `json:"beginLSN,omitempty"`

	// The ending xlog
	EndLSN string `json:"endLSN,omitempty"`

	// The detected error
	Error string `json:"error,omitempty"`

	// Unused. Retained for compatibility with old versions.
	CommandOutput string `json:"commandOutput,omitempty"`

	// The backup command output in case of error
	CommandError string `json:"commandError,omitempty"`

	// Information to identify the instance where the backup has been taken from
	InstanceID *InstanceID `json:"instanceID,omitempty"`
}

// InstanceID contains the information to identify an instance
type InstanceID struct {
	// The pod name
	PodName string `json:"podName,omitempty"`
	// The container ID
	ContainerID string `json:"ContainerID,omitempty"`
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
	err error,
) {
	backupStatus.Phase = BackupPhaseFailed

	if err != nil {
		backupStatus.Error = err.Error()
	} else {
		backupStatus.Error = ""
	}
}

// SetAsCompleted marks a certain backup as completed
func (backupStatus *BackupStatus) SetAsCompleted() {
	backupStatus.Phase = BackupPhaseCompleted
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

// GetMetadata get the metadata
func (backup *Backup) GetMetadata() *metav1.ObjectMeta {
	return &backup.ObjectMeta
}

// GetName get the backup name
func (backup *Backup) GetName() string {
	return backup.Name
}

// GetNamespace get the backup namespace
func (backup *Backup) GetNamespace() string {
	return backup.Namespace
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
