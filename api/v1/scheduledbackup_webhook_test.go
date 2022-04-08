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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
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
