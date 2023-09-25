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
	"errors"
	"fmt"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ErrUnexpectedFencedInstances is raised when there are more than one Pod fenced or when
// there are no Pod fenced at all.
var ErrUnexpectedFencedInstances = errors.New("expected one and only one instance to be fenced")

// volumeSnapshotInfo host information about a volume snapshot
type volumeSnapshotInfo struct {
	// Error contains the raised error when the volume snapshot terminated
	// with a failure
	Error error

	// Running is true when the volume snapshot is running or when we are
	// waiting for the external snapshotter operator to reconcile it
	Running bool
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
		return "non specified volume snapshot error"
	}
	return *err.InternalError.Message
}

// Slice represents a slice of []storagesnapshotv1.VolumeSnapshot
type Slice []storagesnapshotv1.VolumeSnapshot

// GetSnapshotsInterval gets the earliest and latest creation times from a list of VolumeSnapshots
func (s Slice) GetSnapshotsInterval() (metav1.Time, metav1.Time) {
	var firstCreation, lastCreation metav1.Time
	for idx := range s {
		volumeSnapshot := &s[idx]
		if firstCreation.IsZero() || lastCreation.IsZero() {
			firstCreation = volumeSnapshot.CreationTimestamp
			lastCreation = volumeSnapshot.CreationTimestamp
			continue
		}
		if volumeSnapshot.CreationTimestamp.Before(&firstCreation) {
			firstCreation = volumeSnapshot.CreationTimestamp
		}
		if lastCreation.Before(&volumeSnapshot.CreationTimestamp) {
			lastCreation = volumeSnapshot.CreationTimestamp
		}
	}
	return firstCreation, lastCreation
}

// GetControldata retrieves the pg_controldata stored as an annotation in VolumeSnapshots
func (s Slice) GetControldata() (string, error) {
	for _, volumeSnapshot := range s {
		pgControlData, ok := volumeSnapshot.Annotations[utils.PgControldataAnnotationName]
		if !ok {
			continue
		}
		return pgControlData, nil
	}
	return "", fmt.Errorf("could not retrieve pg_controldata from any snapshot")
}

// GetBackupVolumeSnapshots extracts the list of volume snapshots related
// to a backup name
func GetBackupVolumeSnapshots(
	ctx context.Context,
	cli client.Client,
	namespace string,
	backupLabelName string,
) (Slice, error) {
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
			Error:   nil,
			Running: true,
		}
	}

	if snapshot.Status.Error != nil {
		return volumeSnapshotInfo{
			Error: &volumeSnapshotError{
				InternalError: *snapshot.Status.Error,
				Name:          snapshot.Name,
				Namespace:     snapshot.Namespace,
			},
			Running: false,
		}
	}

	if snapshot.Status.ReadyToUse == nil || !*snapshot.Status.ReadyToUse {
		// This volume snapshot completed correctly
		return volumeSnapshotInfo{
			Error:   nil,
			Running: true,
		}
	}

	return volumeSnapshotInfo{
		Error:   nil,
		Running: false,
	}
}
