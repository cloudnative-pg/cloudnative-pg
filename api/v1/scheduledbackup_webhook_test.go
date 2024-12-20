/*
Copyright The CloudNativePG Contributors

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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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

		warnings, result := schedule.validate()
		Expect(warnings).To(BeEmpty())
		Expect(result).To(BeEmpty())
	})

	It("warn the user if the schedule has a wrong number of arguments", func() {
		schedule := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Schedule: "1 2 3 4 5",
			},
		}

		warnings, result := schedule.validate()
		Expect(warnings).To(HaveLen(1))
		Expect(result).To(BeEmpty())
	})

	It("complain with a wrong time", func() {
		schedule := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Schedule: "0 0 0 * * * 1996",
			},
		}

		warnings, result := schedule.validate()
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if VolumeSnapshot CRD is present", func() {
		schedule := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Method:   BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(true)

		warnings, result := schedule.validate()
		Expect(warnings).To(BeEmpty())
		Expect(result).To(BeEmpty())
	})

	It("complains if VolumeSnapshot CRD is not present", func() {
		schedule := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Method:   BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(false)
		warnings, result := schedule.validate()
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.method"))
	})
})
