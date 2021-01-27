/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate schedule", func() {
	It("doesn't complain if there's a schedule", func() {
		schedule := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
			},
		}

		result := schedule.validateSchedule()
		Expect(result).To(BeEmpty())
	})

	It("complain with a wrong time", func() {
		schedule := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Schedule: "0 0 0 * * * 1996",
			},
		}

		result := schedule.validateSchedule()
		Expect(len(result)).To(Equal(1))
	})
})
