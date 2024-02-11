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
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/api/v1/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetSnapshotsBackupTimes gets the time of the oldest and newest snapshots for the cluster
func GetSnapshotsBackupTimes(
	ctx context.Context,
	cli client.Client,
	namespace string,
	clusterName string,
) (*time.Time, *time.Time, error) {
	var list storagesnapshotv1.VolumeSnapshotList
	if err := cli.List(
		ctx,
		&list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			resources.ClusterLabelName: clusterName,
		},
	); err != nil {
		return nil, nil, err
	}

	dataVolSnapshots := make([]storagesnapshotv1.VolumeSnapshot, 0, len(list.Items))
	for _, snapshot := range list.Items {
		if snapshot.Annotations[resources.PvcRoleLabelName] == string(utils.PVCRolePgData) {
			dataVolSnapshots = append(dataVolSnapshots, snapshot)
		}
	}

	if len(dataVolSnapshots) == 0 {
		return nil, nil, nil
	}
	var oldestSnapshot, newestSnapshot *time.Time
	for _, volumeSnapshot := range dataVolSnapshots {
		endTimeStr, hasTime := volumeSnapshot.Annotations[resources.BackupEndTimeAnnotationName]
		if hasTime {
			endTime, err := time.Parse(time.RFC3339, endTimeStr)
			if err != nil {
				return nil, nil, err
			}
			if oldestSnapshot == nil || endTime.Before(*oldestSnapshot) {
				oldestSnapshot = &endTime
			}
			if newestSnapshot == nil || newestSnapshot.Before(endTime) {
				newestSnapshot = &endTime
			}
		}
	}
	return oldestSnapshot, newestSnapshot, nil
}
