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

package persistentvolumeclaim

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ErrUnknownRole is raised when asking a storage source
// for a PVC role that is not known
var ErrUnknownRole = fmt.Errorf("unknown PVC role")

// StorageSource the storage source to be used when creating a set
// of PVCs
type StorageSource struct {
	// The data source that should be used for PGDATA
	DataSource corev1.TypedLocalObjectReference `json:"dataSource"`

	// The (optional) data source that should be used for WALs
	WALSource *corev1.TypedLocalObjectReference `json:"WALSource"`
}

// GetCandidateStorageSource gets the candidate storage source
// to be used to create a PVC
func GetCandidateStorageSource(
	cluster *apiv1.Cluster,
	backupList apiv1.BackupList,
) *StorageSource {
	result := getCandidateSourceFromBackupList(backupList)
	if result != nil {
		return result
	}

	if cluster.Spec.Bootstrap == nil {
		return nil
	}

	if cluster.Spec.Bootstrap.Recovery == nil {
		return nil
	}

	if cluster.Spec.Bootstrap.Recovery.VolumeSnapshots == nil {
		return nil
	}

	volumeSnapshots := cluster.Spec.Bootstrap.Recovery.VolumeSnapshots
	return &StorageSource{
		DataSource: volumeSnapshots.Storage,
		WALSource:  volumeSnapshots.WalStorage,
	}
}

// getCandidateSourceFromBackupList gets a candidate storage source
// given a backup list
func getCandidateSourceFromBackupList(backupList apiv1.BackupList) *StorageSource {
	backupList.SortByReverseCreationTime()
	for _, backup := range backupList.Items {
		backup := backup
		if !isBackupCandidate(&backup) {
			fmt.Println(backup.Name, " is not candidate")
			continue
		}
		fmt.Println(backup.Name, " is candidate")

		result := &StorageSource{
			DataSource: corev1.TypedLocalObjectReference{
				APIGroup: ptr.To("snapshot.storage.k8s.io"),
				Kind:     "VolumeSnapshot",
				Name:     backup.Name,
			},
		}
		if len(backup.Status.BackupSnapshotStatus.Snapshots) > 1 {
			result.WALSource = &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To("snapshot.storage.k8s.io"),
				Kind:     "VolumeSnapshot",
				Name:     fmt.Sprintf("%s-wal", backup.Name), // TODO: remove from here, isolate
			}
		}

		return result
	}

	return nil
}

// isBackupCandidate checks if a backup can be used to bootstrap a
// PVC
func isBackupCandidate(backup *apiv1.Backup) bool {
	if backup.Spec.Method != apiv1.BackupMethodVolumeSnapshot {
		return false
	}

	if backup.Status.Phase != apiv1.BackupPhaseCompleted {
		return false
	}

	return true
}

// ForRole gets the storage source given a PVC role
func (source *StorageSource) ForRole(role utils.PVCRole) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}

	switch role {
	case utils.PVCRolePgData:
		return &source.DataSource, nil
	case utils.PVCRolePgWal:
		return source.WALSource, nil
	default:
		return nil, ErrUnknownRole
	}
}
