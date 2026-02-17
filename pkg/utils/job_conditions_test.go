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

	failedJob := batchv1.Job{
		Status: batchv1.JobStatus{
			Succeeded: 0,
			Conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobFailed,
					Status: "True",
				},
			},
		},
	}

	runningJob := batchv1.Job{
		Status: batchv1.JobStatus{
			Succeeded: 0,
			Conditions: []batchv1.JobCondition{},
		},
	}

	It("detects if a certain job is completed", func() {
		Expect(JobHasOneCompletion(nonCompleteJob)).To(BeFalse())
		Expect(JobHasOneCompletion(completeJob)).To(BeTrue())
	})

	It("detects if a certain job has failed", func() {
		Expect(IsJobFailed(failedJob)).To(BeTrue())
		Expect(IsJobFailed(runningJob)).To(BeFalse())
		Expect(IsJobFailed(completeJob)).To(BeFalse())
		Expect(IsJobFailed(nonCompleteJob)).To(BeFalse())
	})
})
