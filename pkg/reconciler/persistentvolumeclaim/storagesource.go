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

package persistentvolumeclaim

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// StorageSource the storage source to be used when creating a set
// of PVCs
type StorageSource struct {
	// The data source that should be used for PGDATA
	DataSource corev1.TypedLocalObjectReference `json:"dataSource"`

	// The (optional) data source that should be used for WALs
	WALSource *corev1.TypedLocalObjectReference `json:"walSource"`

	// The (optional) data source that should be used for TABLESPACE
	TablespaceSource map[string]corev1.TypedLocalObjectReference `json:"tablespaceSource"`
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

	// Unless WAL archiving is active (via BarmanObjectStore or a WAL-archiver plugin),
	// we can't recover a replica from a backup
	walArchivingActive := (cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil) ||
		cluster.GetEnabledWALArchivePluginName() != ""
	if !walArchivingActive {
		return nil
	}

	if result := getCandidateSourceFromBackupList(
		ctx,
		cluster,
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
	cluster *apiv1.Cluster,
	backupList apiv1.BackupList,
) *StorageSource {
	contextLogger := log.FromContext(ctx)

	majorVersion, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		contextLogger.Warning(
			"unable to determine cluster major version; skipping backup as a recovery source",
			"error", err.Error(),
		)
		return nil
	}

	isCorrectMajorVersion := func(backup *apiv1.Backup) bool {
		// If we don't have image info, we can't determine the cluster version reliably; skip enforcement
		if cluster.Status.PGDataImageInfo == nil {
			return true
		}

		backupMajorVersion := backup.Status.MajorVersion
		if backupMajorVersion == 0 {
			contextLogger.Warning(
				"majorVersion on backup status is not populated, cannot use it as a recovery source.",
			)
			return false
		}

		return majorVersion == backupMajorVersion
	}

	backupList.SortByReverseCreationTime()
	for idx := range backupList.Items {
		backup := &backupList.Items[idx]

		if !backup.IsCompletedVolumeSnapshot() {
			contextLogger.Trace("skipping backup, not a valid storage source candidate")
			continue
		}

		if backup.CreationTimestamp.Before(&cluster.CreationTimestamp) {
			contextLogger.Info(
				"skipping backup as a potential recovery storage source candidate because it was created before the Cluster object",
			)
			continue
		}

		if !isCorrectMajorVersion(backup) {
			contextLogger.Info(
				"skipping backup as a potential recovery storage source candidate because of major version mismatch",
			)
			continue
		}

		source := getCandidateSourceFromBackup(backup)
		if cluster.HasExcludedSnapshot(source.DataSource.Name) {
			contextLogger.Info(
				"skipping backup because its snapshot is excluded from recovery",
				"backup", backup.Name,
				"snapshot", source.DataSource.Name,
			)
			continue
		}

		contextLogger.Debug("found a backup that is a valid storage source candidate")
		return source
	}

	return nil
}

func getCandidateSourceFromBackup(backup *apiv1.Backup) *StorageSource {
	var result StorageSource
	for _, element := range backup.Status.BackupSnapshotStatus.Elements {
		reference := corev1.TypedLocalObjectReference{
			APIGroup: ptr.To(volumesnapshotv1.GroupName),
			Kind:     apiv1.VolumeSnapshotKind,
			Name:     element.Name,
		}
		switch utils.PVCRole(element.Type) {
		case utils.PVCRolePgData:
			result.DataSource = reference
		case utils.PVCRolePgWal:
			result.WALSource = &reference
		case utils.PVCRolePgTablespace:
			if result.TablespaceSource == nil {
				result.TablespaceSource = map[string]corev1.TypedLocalObjectReference{}
			}
			result.TablespaceSource[element.TablespaceName] = reference
		}
	}

	return &result
}

// getCandidateSourceFromClusterDefinition gets a candidate storage source
// from a Cluster definition, taking into consideration the backup that the
// cluster has been bootstrapped from
func getCandidateSourceFromClusterDefinition(cluster *apiv1.Cluster) *StorageSource {
	if cluster.Spec.Bootstrap == nil ||
		cluster.Spec.Bootstrap.Recovery == nil ||
		cluster.Spec.Bootstrap.Recovery.VolumeSnapshots == nil {
		return nil
	}

	volumeSnapshots := cluster.Spec.Bootstrap.Recovery.VolumeSnapshots
	source := &StorageSource{
		DataSource:       volumeSnapshots.Storage,
		WALSource:        volumeSnapshots.WalStorage,
		TablespaceSource: volumeSnapshots.TablespaceStorage,
	}
	if cluster.HasExcludedSnapshot(source.DataSource.Name) {
		return nil
	}
	return source
}
