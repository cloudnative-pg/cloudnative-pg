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
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetOldestSnapshot gets the time of the oldest snapshot for the cluster
func GetOldestSnapshot(
	ctx context.Context,
	cli client.Client,
	namespace string,
	clusterName string,
) (time.Time, error) {
	var oldestSnaphsot time.Time

	var list storagesnapshotv1.VolumeSnapshotList
	if err := cli.List(
		ctx,
		&list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName: clusterName,
		},
	); err != nil {
		return oldestSnaphsot, err
	}

	dataVolSnapshots := make([]storagesnapshotv1.VolumeSnapshot, 0, len(list.Items))
	for _, snapshot := range list.Items {
		if snapshot.Annotations[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
			dataVolSnapshots = append(dataVolSnapshots, snapshot)
		}
	}

	if len(dataVolSnapshots) == 0 {
		return oldestSnaphsot, fmt.Errorf("there were no snapshots")
	}
	for _, volumeSnapshot := range dataVolSnapshots {
		endTimeStr, hasTime := volumeSnapshot.Annotations[utils.BackupEndTimeAnnotationName]
		if hasTime {
			endTime, err := time.Parse(time.RFC3339, endTimeStr)
			if err != nil {
				return oldestSnaphsot, err
			}
			if oldestSnaphsot.IsZero() || endTime.Before(oldestSnaphsot) {
				oldestSnaphsot = endTime
			}
		}
	}
	if oldestSnaphsot.IsZero() {
		return oldestSnaphsot, fmt.Errorf("no backup end time annotations found")
	}
	return oldestSnaphsot, nil
}
