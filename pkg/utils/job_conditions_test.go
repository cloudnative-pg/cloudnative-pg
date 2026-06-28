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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job conditions", func() {
	nonCompleteJob := batchv1.Job{
		Status: batchv1.JobStatus{
			Succeeded: 0,
		},
	}

	completeJob := batchv1.Job{
		Status: batchv1.JobStatus{
			Succeeded: 1,
		},
	}

	It("detects if a certain job is completed", func() {
		Expect(JobHasOneCompletion(nonCompleteJob)).To(BeFalse())
		Expect(JobHasOneCompletion(completeJob)).To(BeTrue())
	})

	DescribeTable("detects if a job has permanently failed",
		func(job batchv1.Job, expected bool) {
			Expect(JobHasFailed(job)).To(Equal(expected))
		},
		Entry("a job that has not started yet", batchv1.Job{}, false),
		Entry("a running job with one failed pod still within its backoff limit",
			batchv1.Job{Status: batchv1.JobStatus{Failed: 1}}, false),
		Entry("a successfully completed job",
			batchv1.Job{Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
				},
			}}, false),
		Entry("a job that reached its backoff limit",
			batchv1.Job{Status: batchv1.JobStatus{
				Failed: 7,
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"},
				},
			}}, true),
		Entry("a job whose failure condition is not active",
			batchv1.Job{Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionFalse},
				},
			}}, false),
		Entry("a job that is past its backoff limit but whose pods are still terminating",
			batchv1.Job{Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"},
				},
			}}, false),
	)
})
