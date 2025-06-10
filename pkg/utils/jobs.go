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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// IsJobFailed check if a job has failed
func IsJobFailed(job batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsJobStuck checks if a job is stuck in pending state for too long
func IsJobStuck(job batchv1.Job, timeout time.Duration) bool {
	// If the job is already marked as failed or complete, it's not stuck
	if IsJobFailed(job) || IsJobComplete(job) {
		return false
	}

	// Check if job has been pending for too long
	if job.CreationTimestamp.Add(timeout).Before(time.Now()) {
		// Check if any pods are unschedulable
		if job.Status.Active == 0 && job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			// No pods have been created or they're all unschedulable
			return true
		}
	}

	return false
}

// IsJobComplete checks if a job has completed successfully
func IsJobComplete(job batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsJobFailedOrStuck checks if a job has failed or is stuck
func IsJobFailedOrStuck(job batchv1.Job, stuckTimeout time.Duration) bool {
	return IsJobFailed(job) || IsJobStuck(job, stuckTimeout)
}
