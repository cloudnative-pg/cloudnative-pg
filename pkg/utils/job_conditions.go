/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
