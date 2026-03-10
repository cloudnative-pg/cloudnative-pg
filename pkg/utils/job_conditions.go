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

package utils

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// JobHasOneCompletion Completion check if a certain job is complete
func JobHasOneCompletion(job batchv1.Job) bool {
	requestedCompletions := int32(1)
	if job.Spec.Completions != nil {
		requestedCompletions = *job.Spec.Completions
	}
	return job.Status.Succeeded == requestedCompletions
}

// IsJobFailed checks if a job has failed
func IsJobFailed(job batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// FilterJobsWithOneCompletion returns jobs that have one completion
func FilterJobsWithOneCompletion(jobList []batchv1.Job) []batchv1.Job {
	var result []batchv1.Job
	for _, job := range jobList {
		if JobHasOneCompletion(job) {
			result = append(result, job)
		}
	}
	return result
}
