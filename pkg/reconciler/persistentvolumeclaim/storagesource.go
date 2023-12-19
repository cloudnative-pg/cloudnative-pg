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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// GetCandidateStorageSourceForPrimary gets the candidate storage source
// to be used to create a primary PVC
func GetCandidateStorageSourceForPrimary(
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) *StorageSource {
	if backup.IsCompletedVolumeSnapshot() {
		return getCandidateSourceFromBackup(backup)
	}
	return getCandidateSourceFromClusterDefinition(cluster)
}

// GetCandidateStorageSourceForReplica gets the candidate storage source
// to be used to create a replica PVC
func GetCandidateStorageSourceForReplica(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backupList apiv1.BackupList,
) *StorageSource {
	// We can't use a Backup to create a replica when:
	//
	// 1. we don't have WAL archiving, because the backup may be old
	//    and the primary may not have the WAL files needed for the
	//    new replica to be in-sync
	//
	// 2. we need two different WAL object stores, because we cannot
	//    access them at the same time. This can happen when we have:
	//
	//    - the object store where we upload the WAL files
	//      i.e. `.spec.backup.barmanObjectStore`
	//
	//    - the object store where were we aed WAL files to create the
	//      bootstrap primary instance
	//      i.e. `.spec.externalClusters[i].barmanObjectStore` and
	//      `.spec.bootstrap.recovery.source`
	//
	//    This is true only for the backup that was used to bootstrap
	//    the cluster itself. Other backups are fine because the required
	//    WALs have been archived in the cluster object store.

	// Unless WAL archiving is active, we can't recover a replica from a backup
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		return nil
	}

	if result := getCandidateSourceFromBackupList(
		ctx,
		cluster.ObjectMeta.CreationTimestamp,
		backupList,
	); result != nil {
		return result
	}

	// We support one and only one object store, see comment at the beginning
	// of this function
	if cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.Recovery != nil &&
		len(cluster.Spec.Bootstrap.Recovery.Source) > 0 {
		return nil
	}

	// Try using the backup the Cluster has been bootstrapped from
	return getCandidateSourceFromClusterDefinition(cluster)
}

// getCandidateSourceFromBackupList gets a candidate storage source
// given a backup list
func getCandidateSourceFromBackupList(
	ctx context.Context,
	clusterCreationTime metav1.Time,
	backupList apiv1.BackupList,
) *StorageSource {
	contextLogger := log.FromContext(ctx)

	backupList.SortByReverseCreationTime()
	for idx := range backupList.Items {
		backup := &backupList.Items[idx]
		contextLogger := contextLogger.WithValues()

		if !backup.IsCompletedVolumeSnapshot() {
			contextLogger.Trace("skipping backup, not a valid storage source candidate")
			continue
		}

		if backup.ObjectMeta.CreationTimestamp.Before(&clusterCreationTime) {
			contextLogger.Info(
				"skipping backup as a potential recovery storage source candidate " +
					"because if was created before the Cluster object")
			continue
		}

		contextLogger.Debug("found a backup that is a valid storage source candidate")

		return getCandidateSourceFromBackup(backup)
	}

	return nil
}

func getCandidateSourceFromBackup(backup *apiv1.Backup) *StorageSource {
	var result StorageSource
	for _, element := range backup.Status.BackupSnapshotStatus.Elements {
		reference := corev1.TypedLocalObjectReference{
			APIGroup: ptr.To(volumesnapshot.GroupName),
			Kind:     apiv1.VolumeSnapshotKind,
			Name:     element.Name,
		}
		switch utils.PVCRole(element.Type) {
		case utils.PVCRolePgData:
			result.DataSource = reference
		case utils.PVCRolePgWal:
			result.WALSource = &reference
		}
	}

	return &result
}

// getCandidateSourceFromClusterDefinition gets a candidate storage source
// from a Cluster definition, taking into consideration the backup that the
// cluster has been bootstrapped from
func getCandidateSourceFromClusterDefinition(cluster *apiv1.Cluster) *StorageSource {
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
