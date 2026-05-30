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
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeletePVCsWithMissingVolumeSnapshots detects Pending PVCs whose VolumeSnapshot
// dataSource no longer exists and deletes them (along with any associated Job)
// so the next reconciliation can fall back to pg_basebackup.
func DeletePVCsWithMissingVolumeSnapshots(
	ctx context.Context,
	c client.Client,
	pvcs []corev1.PersistentVolumeClaim,
	jobs []batchv1.Job,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var deletionExecuted bool
	for i := range pvcs {
		deleted, err := deletePVCWithMissingSnapshot(ctx, c, &pvcs[i], jobs)
		if err != nil {
			contextLogger.Error(err, "Error while cleaning up PVC with missing VolumeSnapshot",
				"pvc", pvcs[i].Name)
			continue
		}
		deletionExecuted = deletionExecuted || deleted
	}

	var res ctrl.Result
	if deletionExecuted {
		res = ctrl.Result{RequeueAfter: time.Second}
	}

	return res, nil
}

// deletePVCWithMissingSnapshot checks whether a Pending PVC references a
// VolumeSnapshot that no longer exists. If so, it deletes the PVC and any
// associated Job so the operator can retry replica creation via pg_basebackup.
func deletePVCWithMissingSnapshot(
	ctx context.Context,
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	jobs []batchv1.Job,
) (bool, error) {
	if pvc.Status.Phase != corev1.ClaimPending {
		return false, nil
	}

	snapshotName := getSnapshotDataSourceName(pvc)
	if snapshotName == "" {
		return false, nil
	}

	contextLogger := log.FromContext(ctx)

	var vs volumesnapshotv1.VolumeSnapshot
	err := c.Get(ctx, client.ObjectKey{Namespace: pvc.Namespace, Name: snapshotName}, &vs)
	if err == nil {
		return false, nil
	}
	if !apierrs.IsNotFound(err) {
		contextLogger.Error(err, "Error checking VolumeSnapshot existence, skipping PVC",
			"pvc", pvc.Name, "snapshot", snapshotName)
		return false, nil
	}

	contextLogger.Info(
		"Deleting Pending PVC whose VolumeSnapshot dataSource no longer exists",
		"pvc", pvc.Name,
		"missingSnapshot", snapshotName,
	)

	if job := findJobUsingPVC(jobs, pvc.Name); job != nil {
		contextLogger.Info("Deleting Job associated with stuck PVC",
			"job", job.Name, "pvc", pvc.Name)
		if err := c.Delete(ctx, job); err != nil && !apierrs.IsNotFound(err) {
			return false, err
		}
	}

	if err := c.Delete(ctx, pvc); err != nil && !apierrs.IsNotFound(err) {
		return false, err
	}

	return true, nil
}

// getSnapshotDataSourceName returns the VolumeSnapshot name referenced in the
// PVC's DataSource, or empty string if the DataSource is not a VolumeSnapshot.
func getSnapshotDataSourceName(pvc *corev1.PersistentVolumeClaim) string {
	ds := pvc.Spec.DataSource
	if ds == nil {
		return ""
	}
	if ds.APIGroup == nil || *ds.APIGroup != volumesnapshotv1.GroupName {
		return ""
	}
	return ds.Name
}

// findJobUsingPVC returns the first Job from the list that references the given PVC name.
func findJobUsingPVC(jobs []batchv1.Job, pvcName string) *batchv1.Job {
	for i := range jobs {
		for _, vol := range jobs[i].Spec.Template.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvcName {
				return &jobs[i]
			}
		}
	}
	return nil
}
