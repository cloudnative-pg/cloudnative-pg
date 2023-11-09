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

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
)

type onlineExecutor struct {
	backupClient *webserver.BackupClient
}

func newOnlineExecutor() *onlineExecutor {
	return &onlineExecutor{backupClient: webserver.NewBackupClient()}
}

func (o *onlineExecutor) finalize(
	ctx context.Context,
	_ *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	status, err := o.backupClient.Status(ctx, targetPod.Status.PodIP)
	if err != nil {
		return nil, fmt.Errorf("while getting status: %w", err)
	}

	if status.BackupName != backup.Name {
		return nil, fmt.Errorf("trying to stop backup with name: %s, while reconciling backup with name: %s",
			status.BackupName,
			backup.Name,
		)
	}

	switch status.Phase {
	case webserver.Started:
		if err := o.backupClient.Stop(ctx, targetPod.Status.PodIP); err != nil {
			return nil, fmt.Errorf("while stopping the backup client: %w", err)
		}
		return &ctrl.Result{RequeueAfter: time.Second * 5}, nil
	case webserver.Closing:
		return &ctrl.Result{RequeueAfter: time.Second * 5}, nil
	case webserver.Completed:
		backup.Status.BeginLSN = string(status.BeginLSN)
		backup.Status.EndLSN = string(status.EndLSN)
		backup.Status.TablespaceMapFile = status.SpcmapFile
		backup.Status.BackupLabelFile = status.LabelFile

		return nil, nil
	default:
		return nil, fmt.Errorf(
			"found the instance in an unexpected state while finalizing the backup, phase: %s",
			status.Phase,
		)
	}
}

func (o *onlineExecutor) prepare(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	volumeSnapshotConfig := backup.GetVolumeSnapshotConfiguration(*cluster.Spec.Backup.VolumeSnapshot)

	// Handle hot snapshots
	status, err := o.backupClient.Status(ctx, targetPod.Status.PodIP)
	if err != nil {
		return nil, fmt.Errorf("while getting status: %w", err)
	}
	switch {
	// if the backupName doesn't match it means we have an old stuck pending backup that we have to force out.
	case status.Phase == "", backup.Name != status.BackupName:
		req := webserver.StartBackupRequest{
			ImmediateCheckpoint: volumeSnapshotConfig.OnlineConfiguration.GetImmediateCheckpoint(),
			WaitForArchive:      volumeSnapshotConfig.OnlineConfiguration.GetWaitForArchive(),
			BackupName:          backup.Name,
			Force:               true,
		}
		if _, err := o.backupClient.Start(ctx, targetPod.Status.PodIP, req); err != nil {
			return nil, fmt.Errorf("while trying to start the backup: %w", err)
		}
	case status.Phase == webserver.Starting:
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	case status.Phase == webserver.Started:
		return nil, nil
	}

	return nil, fmt.Errorf("found zero snapshot but the instance is in phase: %s", status.Phase)
}
