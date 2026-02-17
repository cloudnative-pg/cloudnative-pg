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

	"github.com/cloudnative-pg/machinery/pkg/log"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// cleanupCompletedJobs removes all Jobs which are completed.
// Note: Failed jobs are intentionally left intact for troubleshooting
// and to serve as markers for fallback logic (e.g., snapshot recovery fallback).
func (r *ClusterReconciler) cleanupCompletedJobs(
	ctx context.Context,
	jobs batchv1.JobList,
) {
	contextLogger := log.FromContext(ctx)

	foreground := metav1.DeletePropagationForeground
	completedJobs := utils.FilterJobsWithOneCompletion(jobs.Items)
	for idx := range completedJobs {
		job := &completedJobs[idx]
		if !job.DeletionTimestamp.IsZero() {
			contextLogger.Debug("skipping job because it has deletion timestamp populated",
				"job", job.Name)
			continue
		}

		contextLogger.Debug("Removing job", "job", job.Name)
		if err := r.Delete(ctx, job, &client.DeleteOptions{
			PropagationPolicy: &foreground,
		}); err != nil {
			contextLogger.Error(err, "cannot delete job", "job", job.Name)
			continue
		}
	}
}
