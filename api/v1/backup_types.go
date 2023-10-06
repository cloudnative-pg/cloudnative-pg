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
	"context"
	"sort"
	"strings"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
)

// BackupSpec defines the desired state of Backup
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

	// The backup method to be used, possible options are `barmanObjectStore`
	// and `volumeSnapshot`. Defaults to: `barmanObjectStore`.
	// +optional
	// +kubebuilder:validation:Enum=barmanObjectStore;volumeSnapshot
	// +kubebuilder:default:=barmanObjectStore
	Method BackupMethod `json:"method,omitempty"`
}

// BackupSnapshotStatus the fields exclusive to the volumeSnapshot method backup
type BackupSnapshotStatus struct {
	// The snapshot lists, populated if it is a snapshot type backup
	// +optional
	Snapshots []string `json:"snapshots,omitempty"`
}

// BackupStatus defines the observed state of Backup
type BackupStatus struct {
	// The potential credentials for each cloud provider
	BarmanCredentials `json:",inline"`

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

	// Information to identify the instance where the backup has been taken from
	// +optional
	InstanceID *InstanceID `json:"instanceID,omitempty"`

	// Status of the volumeSnapshot backup
	// +optional
	BackupSnapshotStatus BackupSnapshotStatus `json:"snapshotBackupStatus,omitempty"`

	// The backup method being used
	// +optional
	Method BackupMethod `json:"method,omitempty"`
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

// Backup is the Schema for the backups API
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
	backupStatus.StoppedAt = ptr.To(metav1.Now())
}

// SetAsStarted marks a certain backup as started
func (backupStatus *BackupStatus) SetAsStarted(targetPod *corev1.Pod, method BackupMethod) {
	backupStatus.Phase = BackupPhaseStarted
	backupStatus.InstanceID = &InstanceID{
		PodName:     targetPod.Name,
		ContainerID: targetPod.Status.ContainerStatuses[0].ContainerID,
	}
	backupStatus.Method = method
}

// SetSnapshotList sets the Snapshots field from a list of VolumeSnapshot
func (snapshotStatus *BackupSnapshotStatus) SetSnapshotList(snapshots []volumesnapshot.VolumeSnapshot) {
	snapshotNames := make([]string, len(snapshots))
	for idx, volumeSnapshot := range snapshots {
		snapshotNames[idx] = volumeSnapshot.Name
	}
	snapshotStatus.Snapshots = snapshotNames
}

// IsDone check if a backup is completed or still in progress
func (backupStatus *BackupStatus) IsDone() bool {
	return backupStatus.Phase == BackupPhaseCompleted || backupStatus.Phase == BackupPhaseFailed
}

// IsInProgress check if a certain backup is in progress or not
func (backupStatus *BackupStatus) IsInProgress() bool {
	return backupStatus.Phase == BackupPhasePending ||
		backupStatus.Phase == BackupPhaseStarted ||
		backupStatus.Phase == BackupPhaseRunning
}

// GetPendingBackupNames returns the pending backup list
func (list BackupList) GetPendingBackupNames() []string {
	// Retry the backup if another backup is running
	pendingBackups := make([]string, 0, len(list.Items))
	for _, concurrentBackup := range list.Items {
		if concurrentBackup.Status.IsDone() {
			continue
		}
		if !concurrentBackup.Status.IsInProgress() {
			pendingBackups = append(pendingBackups, concurrentBackup.Name)
		}
	}

	return pendingBackups
}

// CanExecuteBackup control if we can start a reconciliation loop for a certain backup.
//
// A reconciliation loop can start if:
// - there's no backup running, and if the first of the sorted list of backups
// - the current backup is running and is the first running backup of the list
//
// As a side effect, this function will sort the backup list
func (list *BackupList) CanExecuteBackup(backupName string) bool {
	var foundRunningBackup bool

	list.SortByName()

	for _, concurrentBackup := range list.Items {
		if concurrentBackup.Status.IsInProgress() {
			if backupName == concurrentBackup.Name && !foundRunningBackup {
				return true
			}

			foundRunningBackup = true
			if backupName != concurrentBackup.Name {
				return false
			}
		}
	}

	pendingBackups := list.GetPendingBackupNames()
	if len(pendingBackups) > 0 && pendingBackups[0] != backupName {
		return false
	}

	return true
}

// SortByName sorts the backup items in alphabetical order
func (list *BackupList) SortByName() {
	// Sort the list of backups in alphabetical order
	sort.Slice(list.Items, func(i, j int) bool {
		return strings.Compare(list.Items[i].Name, list.Items[j].Name) <= 0
	})
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

// GetAssignedInstance fetches the instance that was assigned to the backup execution
func (backup *Backup) GetAssignedInstance(ctx context.Context, cli client.Client) (*corev1.Pod, error) {
	if backup.Status.InstanceID == nil || len(backup.Status.InstanceID.PodName) == 0 {
		return nil, nil
	}

	var previouslyElectedPod corev1.Pod
	if err := cli.Get(
		ctx,
		client.ObjectKey{Namespace: backup.Namespace, Name: backup.Status.InstanceID.PodName},
		&previouslyElectedPod,
	); err != nil {
		return nil, err
	}

	return &previouslyElectedPod, nil
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
