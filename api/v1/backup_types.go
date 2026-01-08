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
	barmanApi "github.com/cloudnative-pg/barman-cloud/pkg/api"
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

	// BackupPhaseFinalizing means that a consistent backup have been
	// taken and the operator is waiting for it to be ready to be
	// used to restore a cluster.
	// This phase is used for VolumeSnapshot backups, when a
	// VolumeSnapshotContent have already been provisioned, but it is
	// still now waiting for the `readyToUse` flag to be true.
	BackupPhaseFinalizing = "finalizing"

	// BackupPhaseCompleted means that the backup is now completed
	BackupPhaseCompleted = "completed"

	// BackupPhaseFailed means that the backup is failed
	BackupPhaseFailed = "failed"

	// BackupPhaseWalArchivingFailing means wal archiving isn't properly working
	BackupPhaseWalArchivingFailing = "walArchivingFailing"
)

// BarmanCredentials an object containing the potential credentials for each cloud provider
// +kubebuilder:object:generate:=false
type BarmanCredentials = barmanApi.BarmanCredentials

// AzureCredentials is the type for the credentials to be used to upload
// files to Azure Blob Storage. The connection string contains every needed
// information. If the connection string is not specified, one (and only one)
// of the following authentication methods must be specified:
//
// - storageKey (requires storageAccount)
// - storageSasToken (requires storageAccount)
// - inheritFromAzureAD (inheriting credentials from the pod environment)
// - useDefaultAzureCredentials (using the default Azure authentication flow)
//
// +kubebuilder:object:generate:=false
type AzureCredentials = barmanApi.AzureCredentials

// BarmanObjectStoreConfiguration contains the backup configuration
// using Barman against an S3-compatible object storage
// +kubebuilder:object:generate:=false
type BarmanObjectStoreConfiguration = barmanApi.BarmanObjectStoreConfiguration

// DataBackupConfiguration is the configuration of the backup of
// the data directory
// +kubebuilder:object:generate:=false
type DataBackupConfiguration = barmanApi.DataBackupConfiguration

// GoogleCredentials is the type for the Google Cloud Storage credentials.
// This needs to be specified even if we run inside a GKE environment.
// +kubebuilder:object:generate:=false
type GoogleCredentials = barmanApi.GoogleCredentials

// S3Credentials is the type for the credentials to be used to upload
// files to S3. It can be provided in two alternative ways:
//
// - explicitly passing accessKeyId and secretAccessKey
//
// - inheriting the role from the pod environment by setting inheritFromIAMRole to true
// +kubebuilder:object:generate:=false
type S3Credentials = barmanApi.S3Credentials

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
// +kubebuilder:object:generate:=false
type WalBackupConfiguration = barmanApi.WalBackupConfiguration

// BackupMethod defines the way of executing the physical base backups of
// the selected PostgreSQL instance
type BackupMethod string

const (
	// BackupMethodVolumeSnapshot means using the volume snapshot
	// Kubernetes feature
	BackupMethodVolumeSnapshot BackupMethod = "volumeSnapshot"

	// BackupMethodBarmanObjectStore means using barman to backup the
	// PostgreSQL cluster
	BackupMethodBarmanObjectStore BackupMethod = "barmanObjectStore"

	// BackupMethodPlugin means that this backup should be handled by
	// a plugin
	BackupMethodPlugin BackupMethod = "plugin"
)

// BackupSpec defines the desired state of Backup
// +kubebuilder:validation:XValidation:rule="oldSelf == self",message="BackupSpec is immutable once set"
type BackupSpec struct {
	// The cluster to backup
	Cluster LocalObjectReference `json:"cluster"`

	// The policy to decide which instance should perform this backup. If empty,
	// it defaults to `cluster.spec.backup.target`.
	// Available options are empty string, `primary` and `prefer-standby`.
	// `primary` to have backups run always on primary instances,
	// `prefer-standby` to have backups run preferably on the most updated
	// standby, if available.
	// +optional
	// +kubebuilder:validation:Enum=primary;prefer-standby
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

// BackupPluginConfiguration contains the backup configuration used by
// the backup plugin
type BackupPluginConfiguration struct {
	// Name is the name of the plugin managing this backup
	Name string `json:"name"`

	// Parameters are the configuration parameters passed to the backup
	// plugin for this backup
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
}

// BackupSnapshotStatus the fields exclusive to the volumeSnapshot method backup
type BackupSnapshotStatus struct {
	// The elements list, populated with the gathered volume snapshots
	// +optional
	Elements []BackupSnapshotElementStatus `json:"elements,omitempty"`
}

// BackupSnapshotElementStatus is a volume snapshot that is part of a volume snapshot method backup
type BackupSnapshotElementStatus struct {
	// Name is the snapshot resource name
	Name string `json:"name"`

	// Type is tho role of the snapshot in the cluster, such as PG_DATA, PG_WAL and PG_TABLESPACE
	Type string `json:"type"`

	// TablespaceName is the name of the snapshotted tablespace. Only set
	// when type is PG_TABLESPACE
	// +optional
	TablespaceName string `json:"tablespaceName,omitempty"`
}

// BackupStatus defines the observed state of Backup
type BackupStatus struct {
	// The potential credentials for each cloud provider
	barmanApi.BarmanCredentials `json:",inline"`

	// The PostgreSQL major version that was running when the
	// backup was taken.
	MajorVersion int `json:"majorVersion,omitempty"`

	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive.
	// +optional
	EndpointCA *SecretKeySelector `json:"endpointCA,omitempty"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data. This may not be populated in case of errors.
	// +optional
	DestinationPath string `json:"destinationPath,omitempty"`

	// The server name on S3, the cluster name is used if this
	// parameter is omitted
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// Encryption method required to S3 API
	// +optional
	Encryption string `json:"encryption,omitempty"`

	// The ID of the Barman backup
	// +optional
	BackupID string `json:"backupId,omitempty"`

	// The Name of the Barman backup
	// +optional
	BackupName string `json:"backupName,omitempty"`

	// The last backup status
	// +optional
	Phase BackupPhase `json:"phase,omitempty"`

	// When the backup was started
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// When the backup was terminated
	// +optional
	StoppedAt *metav1.Time `json:"stoppedAt,omitempty"`

	// The starting WAL
	// +optional
	BeginWal string `json:"beginWal,omitempty"`

	// The ending WAL
	// +optional
	EndWal string `json:"endWal,omitempty"`

	// The starting xlog
	// +optional
	BeginLSN string `json:"beginLSN,omitempty"`

	// The ending xlog
	// +optional
	EndLSN string `json:"endLSN,omitempty"`

	// The detected error
	// +optional
	Error string `json:"error,omitempty"`

	// Unused. Retained for compatibility with old versions.
	// +optional
	CommandOutput string `json:"commandOutput,omitempty"`

	// The backup command output in case of error
	// +optional
	CommandError string `json:"commandError,omitempty"`

	// Backup label file content as returned by Postgres in case of online (hot) backups
	// +optional
	BackupLabelFile []byte `json:"backupLabelFile,omitempty"`

	// Tablespace map file content as returned by Postgres in case of online (hot) backups
	// +optional
	TablespaceMapFile []byte `json:"tablespaceMapFile,omitempty"`

	// Information to identify the instance where the backup has been taken from
	// +optional
	InstanceID *InstanceID `json:"instanceID,omitempty"`

	// Status of the volumeSnapshot backup
	// +optional
	BackupSnapshotStatus BackupSnapshotStatus `json:"snapshotBackupStatus,omitempty"`

	// The backup method being used
	// +optional
	Method BackupMethod `json:"method,omitempty"`

	// Whether the backup was online/hot (`true`) or offline/cold (`false`)
	// +optional
	Online *bool `json:"online,omitempty"`

	// A map containing the plugin metadata
	// +optional
	PluginMetadata map[string]string `json:"pluginMetadata,omitempty"`
}

// InstanceID contains the information to identify an instance
type InstanceID struct {
	// The pod name
	// +optional
	PodName string `json:"podName,omitempty"`
	// The container ID
	// +optional
	ContainerID string `json:"ContainerID,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="Method",type="string",JSONPath=".spec.method"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.error"

// A Backup resource is a request for a PostgreSQL backup by the user.
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the desired behavior of the backup.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec BackupSpec `json:"spec"`
	// Most recently observed status of the backup. This data may not be up to
	// date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status BackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of backups
	Items []Backup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
