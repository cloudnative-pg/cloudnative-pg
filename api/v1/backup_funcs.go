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
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

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

// SetAsFinalizing marks a certain backup as finalizing
func (backupStatus *BackupStatus) SetAsFinalizing() {
	backupStatus.Phase = BackupPhaseFinalizing
	backupStatus.Error = ""
}

// SetAsCompleted marks a certain backup as completed
func (backupStatus *BackupStatus) SetAsCompleted() {
	backupStatus.Phase = BackupPhaseCompleted
	backupStatus.Error = ""
	backupStatus.StoppedAt = ptr.To(metav1.Now())
}

// SetAsStarted marks a certain backup as started
func (backupStatus *BackupStatus) SetAsStarted(podName, containerID, sessionID string, method BackupMethod) {
	backupStatus.Phase = BackupPhaseStarted
	backupStatus.InstanceID = &InstanceID{
		PodName:     podName,
		ContainerID: containerID,
		SessionID:   sessionID,
	}
	backupStatus.Method = method
}

// SetSnapshotElements sets the Snapshots field from a list of VolumeSnapshot
func (snapshotStatus *BackupSnapshotStatus) SetSnapshotElements(snapshots []volumesnapshotv1.VolumeSnapshot) {
	snapshotNames := make([]BackupSnapshotElementStatus, len(snapshots))
	for idx, volumeSnapshot := range snapshots {
		snapshotNames[idx] = BackupSnapshotElementStatus{
			Name:           volumeSnapshot.Name,
			Type:           volumeSnapshot.Annotations[utils.PvcRoleLabelName],
			TablespaceName: volumeSnapshot.Labels[utils.TablespaceNameLabelName],
		}
	}
	snapshotStatus.Elements = snapshotNames
}

// IsDone check if a backup is completed or still in progress
func (backupStatus *BackupStatus) IsDone() bool {
	return backupStatus.Phase == BackupPhaseCompleted || backupStatus.Phase == BackupPhaseFailed
}

// GetOnline tells whether this backup was taken while the database
// was up
func (backupStatus *BackupStatus) GetOnline() bool {
	if backupStatus.Online == nil {
		return false
	}

	return *backupStatus.Online
}

// GetVolumeSnapshotDeadline returns the volume snapshot deadline in minutes.
func (backup *Backup) GetVolumeSnapshotDeadline() time.Duration {
	const defaultValue = 10

	value := backup.Annotations[utils.BackupVolumeSnapshotDeadlineAnnotationName]
	if value == "" {
		return defaultValue * time.Minute
	}

	minutes, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue * time.Minute
	}

	return time.Duration(minutes) * time.Minute
}

// IsCompletedVolumeSnapshot checks if a backup is completed using the volume snapshot method.
// It returns true if the backup's method is BackupMethodVolumeSnapshot and its status phase is BackupPhaseCompleted.
// Otherwise, it returns false.
func (backup *Backup) IsCompletedVolumeSnapshot() bool {
	return backup != nil &&
		backup.Spec.Method == BackupMethodVolumeSnapshot &&
		backup.Status.Phase == BackupPhaseCompleted
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

// SortByReverseCreationTime sorts the backup items in reverse creation time (starting from the latest one)
func (list *BackupList) SortByReverseCreationTime() {
	// Sort the list of backups in reverse creation time
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].CreationTimestamp.Compare(list.Items[j].CreationTimestamp.Time) > 0
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

// GetOnlineOrDefault returns the online value for the backup.
func (backup *Backup) GetOnlineOrDefault(cluster *Cluster) bool {
	// Offline backups are supported only with the
	// volume snapshot backup method.
	if backup.Spec.Method != BackupMethodVolumeSnapshot {
		return true
	}

	if backup.Spec.Online != nil {
		return *backup.Spec.Online
	}

	if cluster.Spec.Backup == nil || cluster.Spec.Backup.VolumeSnapshot == nil {
		return true
	}

	config := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)
	if config.Online != nil {
		return *config.Online
	}

	return true
}

// GetVolumeSnapshotConfiguration overrides the  configuration value with the ones specified
// in the backup, if present.
func (backup *Backup) GetVolumeSnapshotConfiguration(
	clusterConfig VolumeSnapshotConfiguration,
) VolumeSnapshotConfiguration {
	config := clusterConfig
	if backup.Spec.Online != nil {
		config.Online = backup.Spec.Online
	}

	if backup.Spec.OnlineConfiguration != nil {
		config.OnlineConfiguration = *backup.Spec.OnlineConfiguration
	}

	return config
}

// EnsureGVKIsPresent ensures that the GroupVersionKind (GVK) metadata is present in the Backup object.
// This is necessary because informers do not automatically include metadata inside the object.
// By setting the GVK, we ensure that components such as the plugins have enough metadata to typecheck the object.
func (backup *Backup) EnsureGVKIsPresent() {
	backup.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   SchemeGroupVersion.Group,
		Version: SchemeGroupVersion.Version,
		Kind:    BackupKind,
	})
}

// IsEmpty checks if the plugin configuration is empty or not
func (configuration *BackupPluginConfiguration) IsEmpty() bool {
	return configuration == nil || len(configuration.Name) == 0
}

// IsManagedByInstance returns true if the backup is managed by the instance manager
func (b BackupMethod) IsManagedByInstance() bool {
	return b == BackupMethodPlugin || b == BackupMethodBarmanObjectStore
}

// IsManagedByOperator returns true if the backup is managed by the operator
func (b BackupMethod) IsManagedByOperator() bool {
	return b == BackupMethodVolumeSnapshot
}
