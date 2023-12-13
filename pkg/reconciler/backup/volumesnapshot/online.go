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
	backupClient webserver.BackupClient
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
	body, err := o.backupClient.StatusWithErrors(ctx, targetPod.Status.PodIP)
	if err != nil {
		return nil, fmt.Errorf("while getting status while finalizing: %w", err)
	}

	if err := body.EnsureDataIsPresent(); err != nil {
		return nil, err
	}

	status := body.Data
	if status.BackupName != backup.Name {
		return nil, fmt.Errorf("trying to stop backup with name: %s, while reconciling backup with name: %s",
			status.BackupName,
			backup.Name,
		)
	}

	if status.Phase == webserver.Completed {
		// TODO: eventually move it inside an enrich backup method
		backup.Status.BeginLSN = string(status.BeginLSN)
		backup.Status.EndLSN = string(status.EndLSN)
		backup.Status.TablespaceMapFile = status.SpcmapFile
		backup.Status.BackupLabelFile = status.LabelFile

		return nil, nil
	}

	if body.Error != nil {
		return nil, fmt.Errorf(
			"while processing the finalizing request, phase: %s, "+
				"error message: %s, error code: %s", body.Data.Phase, body.Error.Message, body.Error.Code)
	}

	switch status.Phase {
	case webserver.Started:
		if err := o.backupClient.Stop(ctx,
			targetPod.Status.PodIP,
			*webserver.NewStopBackupRequest(backup.Name)); err != nil {
			return nil, fmt.Errorf("while stopping the backup client: %w", err)
		}
		return &ctrl.Result{RequeueAfter: time.Second * 5}, nil
	case webserver.Closing:
		return &ctrl.Result{RequeueAfter: time.Second * 5}, nil
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
	body, err := o.backupClient.StatusWithErrors(ctx, targetPod.Status.PodIP)
	if err != nil {
		return nil, fmt.Errorf("while getting status while preparing: %w", err)
	}

	if err := body.EnsureDataIsPresent(); err != nil {
		return nil, err
	}

	status := body.Data
	// if the backupName doesn't match it means we have an old stuck pending backup that we have to force out.
	if backup.Name != status.BackupName || status.Phase == "" {
		req := webserver.StartBackupRequest{
			ImmediateCheckpoint: volumeSnapshotConfig.OnlineConfiguration.GetImmediateCheckpoint(),
			WaitForArchive:      volumeSnapshotConfig.OnlineConfiguration.GetWaitForArchive(),
			BackupName:          backup.Name,
			Force:               true,
		}
		if err := o.backupClient.Start(ctx, targetPod.Status.PodIP, req); err != nil {
			return nil, fmt.Errorf("while trying to start the backup: %w", err)
		}
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	switch status.Phase {
	case webserver.Starting:
		return &ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	case webserver.Started:
		return nil, nil
	default:
		return nil, fmt.Errorf("found the instance is an unexpected phase while preparing the snapshot: %s",
			status.Phase)
	}
}
