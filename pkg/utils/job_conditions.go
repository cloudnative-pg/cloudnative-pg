/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package utils

import (
	batchv1 "k8s.io/api/batch/v1"
)

// IsJobComplete check if a certain job is complete
func IsJobComplete(job batchv1.Job) bool {
	requestedCompletions := int32(1)
	if job.Spec.Completions != nil {
		requestedCompletions = *job.Spec.Completions
	}
	return job.Status.Succeeded == requestedCompletions
}

// FilterCompleteJobs returns jobs that are complete
func FilterCompleteJobs(jobList []batchv1.Job) []batchv1.Job {
	var result []batchv1.Job
	for _, job := range jobList {
		if IsJobComplete(job) {
			result = append(result, job)
		}
	}
	return result
}

// CountCompleteJobs count the number complete jobs
func CountCompleteJobs(jobList []batchv1.Job) int {
	result := 0

	for _, job := range jobList {
		if IsJobComplete(job) {
			result++
		}
	}

	return result
}
