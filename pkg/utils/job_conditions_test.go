/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
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
		Expect(IsJobComplete(nonCompleteJob)).To(BeFalse())
		Expect(IsJobComplete(completeJob)).To(BeTrue())
	})

	It("can count the number of complete jobs", func() {
		Expect(CountCompleteJobs([]batchv1.Job{nonCompleteJob, completeJob})).To(Equal(1))
		Expect(CountCompleteJobs([]batchv1.Job{nonCompleteJob})).To(Equal(0))
		Expect(CountCompleteJobs([]batchv1.Job{completeJob})).To(Equal(1))
		Expect(CountCompleteJobs([]batchv1.Job{})).To(Equal(0))
	})
})
