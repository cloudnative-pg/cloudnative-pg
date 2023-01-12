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

package controllers

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

func filterOutDeletedJobs(jobList []batchv1.Job) []batchv1.Job {
	var result []batchv1.Job
	for _, job := range jobList {
		if job.GetDeletionTimestamp() == nil {
			result = append(result, job)
		}
	}
	return result
}

// cleanupCompletedJobs remove all the Jobs which are completed
func (r *ClusterReconciler) cleanupCompletedJobs(
	ctx context.Context,
	jobs batchv1.JobList,
) {
	contextLogger := log.FromContext(ctx)

	completeJobs := utils.FilterCompleteJobs(jobs.Items)
	if len(completeJobs) == 0 {
		return
	}
	jobsToDelete := filterOutDeletedJobs(completeJobs)
	if len(jobsToDelete) == 0 {
		return
	}

	foreground := metav1.DeletePropagationForeground
	for i, job := range jobsToDelete {
		contextLogger.Debug("Removing job", "job", job.Name)

		if err := r.Delete(ctx, &jobsToDelete[i], &client.DeleteOptions{
			PropagationPolicy: &foreground,
		}); err != nil {
			contextLogger.Error(err, "cannot delete job", "job", job.Name)
			continue
		}
	}
}
