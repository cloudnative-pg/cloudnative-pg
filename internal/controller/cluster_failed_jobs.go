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

package controller

import (
	"context"
	"slices"

	"github.com/cloudnative-pg/machinery/pkg/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// handleFailedJobs detects failed jobs, emits warning events, and records
// excluded snapshot names so they are not used for future replica creation.
// Failed jobs are left in place for troubleshooting; their TTL controller
// or the user is responsible for cleanup.
func (r *ClusterReconciler) handleFailedJobs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) error {
	contextLogger := log.FromContext(ctx)

	var snapshotsToAdd []string

	for i := range resources.jobs.Items {
		job := &resources.jobs.Items[i]

		if !utils.IsJobFailed(*job) {
			continue
		}

		role := job.Labels[utils.JobRoleLabelName]
		instanceName := job.Labels[utils.InstanceNameLabelName]
		reason := getJobFailureReason(*job)

		contextLogger.Info("Detected failed job",
			"job", job.Name,
			"role", role,
			"instance", instanceName,
			"reason", reason,
		)

		r.Recorder.Eventf(cluster, "Warning", "FailedJob",
			"Job %s (role: %s, instance: %s) failed: %s",
			job.Name, role, instanceName, reason)

		// For snapshot-recovery jobs, find the VolumeSnapshot name
		// from the instance's PGDATA PVC
		if role == "snapshot-recovery" {
			if snapshotName := getSnapshotNameFromPVCs(
				resources.pvcs.Items, instanceName,
			); snapshotName != "" {
				if !slices.Contains(cluster.Status.ExcludedSnapshots, snapshotName) &&
					!slices.Contains(snapshotsToAdd, snapshotName) {
					snapshotsToAdd = append(snapshotsToAdd, snapshotName)
				}
			}
		}
	}

	if len(snapshotsToAdd) > 0 {
		return status.PatchWithOptimisticLock(
			ctx,
			r.Client,
			cluster,
			func(c *apiv1.Cluster) {
				c.Status.ExcludedSnapshots = append(
					c.Status.ExcludedSnapshots,
					snapshotsToAdd...,
				)
			},
		)
	}

	return nil
}

// getJobFailureReason extracts the failure reason from a job's conditions
func getJobFailureReason(job batchv1.Job) string {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed &&
			condition.Status == corev1.ConditionTrue {
			if condition.Reason != "" {
				return condition.Reason
			}
			return condition.Message
		}
	}
	return "unknown"
}

// getSnapshotNameFromPVCs finds the VolumeSnapshot name used as
// data source for an instance's PGDATA PVC
func getSnapshotNameFromPVCs(
	pvcs []corev1.PersistentVolumeClaim,
	instanceName string,
) string {
	for i := range pvcs {
		pvc := &pvcs[i]
		if pvc.Labels[utils.InstanceNameLabelName] != instanceName {
			continue
		}
		if pvc.Labels[utils.PvcRoleLabelName] != string(utils.PVCRolePgData) {
			continue
		}
		if pvc.Spec.DataSource != nil && pvc.Spec.DataSource.Kind == "VolumeSnapshot" {
			return pvc.Spec.DataSource.Name
		}
	}
	return ""
}
