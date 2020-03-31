/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
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

// CountCompleteJobs count the number of jobs which
// are not complete
func CountCompleteJobs(jobList []batchv1.Job) int {
	result := 0

	for _, job := range jobList {
		if IsJobComplete(job) {
			result++
		}
	}

	return result
}
