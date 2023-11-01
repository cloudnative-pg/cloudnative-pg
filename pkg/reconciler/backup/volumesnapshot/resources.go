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

package volumesnapshot

import (
	"context"
	"fmt"
	"strings"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// volumeSnapshotInfo host information about a volume snapshot
type volumeSnapshotInfo struct {
	// error contains the raised error when the volume snapshot terminated
	// with a failure. The external snapshotter controller will retry
	error *volumeSnapshotError

	// provisioned is true when the volume snapshot have been cut and
	// provisioned. When this is true, the volume snapshot may not still
	// be ready to be used as a source.
	// Some implementations copy the snapshot in a different storage area.
	provisioned bool

	// ready is true when the volume snapshot is complete and ready to
	// be used as a source for a PVC.
	ready bool
}

// volumeSnapshotError is raised when a volume snapshot failed with
// an error
type volumeSnapshotError struct {
	// InternalError is a representation of the error given
	// by the CSI driver
	InternalError storagesnapshotv1.VolumeSnapshotError

	// Name is the name of the VolumeSnapshot object
	Name string

	// Namespace is the namespace of the VolumeSnapshot object
	Namespace string
}

// Error implements the error interface
func (err volumeSnapshotError) Error() string {
	if err.InternalError.Message == nil {
		return "non-specified volume snapshot error"
	}
	return *err.InternalError.Message
}

// IsRetryable returns true if the external snapshotter controller
// will retry taking the snapshot
func (err volumeSnapshotError) isRetryable() bool {
	if err.InternalError.Message == nil {
		return false
	}

	// Obviously this is a heuristic, but unfortunately we don't have
	// the information we need.
	// We're trying to handle the cases where the external-snapshotter
	// controller failed on a conflict with the following error:
	//
	// the object has been modified; please apply your changes to the latest version and try again

	return strings.Contains(*err.InternalError.Message, "try again")
}

// slice represents a slice of []storagesnapshotv1.VolumeSnapshot
type slice []storagesnapshotv1.VolumeSnapshot

// getControldata retrieves the pg_controldata stored as an annotation in VolumeSnapshots
func (s slice) getControldata() (string, error) {
	for _, volumeSnapshot := range s {
		pgControlData, ok := volumeSnapshot.Annotations[utils.PgControldataAnnotationName]
		if !ok {
			continue
		}
		return pgControlData, nil
	}
	return "", fmt.Errorf("could not retrieve pg_controldata from any snapshot")
}

// getBackupVolumeSnapshots extracts the list of volume snapshots related
// to a backup name
func getBackupVolumeSnapshots(
	ctx context.Context,
	cli client.Client,
	namespace string,
	backupLabelName string,
) (slice, error) {
	var list storagesnapshotv1.VolumeSnapshotList

	if err := cli.List(
		ctx,
		&list,
		client.InNamespace(namespace),
		client.MatchingLabels{utils.BackupNameLabelName: backupLabelName},
	); err != nil {
		return nil, err
	}

	return list.Items, nil
}

// parseVolumeSnapshotInfo extracts information from a volume snapshot resource
func parseVolumeSnapshotInfo(snapshot *storagesnapshotv1.VolumeSnapshot) volumeSnapshotInfo {
	if snapshot.Status == nil {
		return volumeSnapshotInfo{
			error:       nil,
			provisioned: false,
			ready:       false,
		}
	}

	if snapshot.Status.Error != nil {
		return volumeSnapshotInfo{
			provisioned: false,
			ready:       false,
			error: &volumeSnapshotError{
				InternalError: *snapshot.Status.Error,
				Name:          snapshot.Name,
				Namespace:     snapshot.Namespace,
			},
		}
	}

	if snapshot.Status.BoundVolumeSnapshotContentName == nil ||
		len(*snapshot.Status.BoundVolumeSnapshotContentName) == 0 ||
		snapshot.Status.CreationTime == nil {
		// The snapshot have not yet been provisioned
		return volumeSnapshotInfo{
			provisioned: false,
			ready:       false,
			error:       nil,
		}
	}

	if snapshot.Status.ReadyToUse == nil || !*snapshot.Status.ReadyToUse {
		// This volume snapshot have been provisioned but it
		// still not be ready to be used as a source
		return volumeSnapshotInfo{
			error:       nil,
			provisioned: true,
			ready:       false,
		}
	}

	// We're done!
	return volumeSnapshotInfo{
		error:       nil,
		provisioned: true,
		ready:       true,
	}
}
