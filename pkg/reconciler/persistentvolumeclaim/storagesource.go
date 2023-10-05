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
	"context"
	"errors"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// StorageSource the storage source to be used when creating a set
// of PVCs
type StorageSource struct {
	// The data source that should be used for PGDATA
	DataSource corev1.TypedLocalObjectReference `json:"dataSource"`

	// The (optional) data source that should be used for WALs
	WALSource *corev1.TypedLocalObjectReference `json:"walSource"`
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
		return nil, errors.New("unknown PVC role for StorageSource")
	}
}

// GetCandidateStorageSource gets the candidate storage source
// to be used to create a PVC
func GetCandidateStorageSource(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backupList apiv1.BackupList,
) *StorageSource {
	if result := getCandidateSourceFromBackupList(ctx, backupList); result != nil {
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
func getCandidateSourceFromBackupList(ctx context.Context, backupList apiv1.BackupList) *StorageSource {
	contextLogger := log.FromContext(ctx)

	backupList.SortByReverseCreationTime()
	for idx := range backupList.Items {
		backup := &backupList.Items[idx]
		if !backup.IsCompletedVolumeSnapshot() {
			contextLogger.Trace("skipping backup, not a valid storage source candidate",
				"backupName", backup.Name)
			continue
		}

		contextLogger.Debug("found a backup that is a valid storage source candidate",
			"backupName", backup.Name)

		result := &StorageSource{
			DataSource: corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshot.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     GetName(backup.Name, utils.PVCRolePgData),
			},
		}
		if len(backup.Status.BackupSnapshotStatus.Snapshots) > 1 {
			result.WALSource = &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshot.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     GetName(backup.Name, utils.PVCRolePgWal),
			}
		}

		return result
	}

	return nil
}
