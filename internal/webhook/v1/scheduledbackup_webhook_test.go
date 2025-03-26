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

package v1

import (
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate schedule", func() {
	var v *ScheduledBackupCustomValidator
	BeforeEach(func() {
		v = &ScheduledBackupCustomValidator{}
	})

	It("doesn't complain if there's a schedule", func() {
		schedule := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
			},
		}

		warnings, result := v.validate(schedule)
		Expect(warnings).To(BeEmpty())
		Expect(result).To(BeEmpty())
	})

	It("warn the user if the schedule has a wrong number of arguments", func() {
		schedule := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "1 2 3 4 5",
			},
		}

		warnings, result := v.validate(schedule)
		Expect(warnings).To(HaveLen(1))
		Expect(result).To(BeEmpty())
	})

	It("complain with a wrong time", func() {
		schedule := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * * 1996",
			},
		}

		warnings, result := v.validate(schedule)
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if VolumeSnapshot CRD is present", func() {
		schedule := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Method:   apiv1.BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(true)

		warnings, result := v.validate(schedule)
		Expect(warnings).To(BeEmpty())
		Expect(result).To(BeEmpty())
	})

	It("complains if VolumeSnapshot CRD is not present", func() {
		schedule := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Method:   apiv1.BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(false)
		warnings, result := v.validate(schedule)
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.method"))
	})

	It("complains if online is set on a barman backup", func() {
		scheduledBackup := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Method:   apiv1.BackupMethodBarmanObjectStore,
				Online:   ptr.To(true),
				Schedule: "* * * * * *",
			},
		}
		warnings, result := v.validate(scheduledBackup)
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.online"))
	})

	It("complains if onlineConfiguration is set on a barman backup", func() {
		scheduledBackup := &apiv1.ScheduledBackup{
			Spec: apiv1.ScheduledBackupSpec{
				Method:              apiv1.BackupMethodBarmanObjectStore,
				OnlineConfiguration: &apiv1.OnlineConfiguration{},
				Schedule:            "* * * * * *",
			},
		}
		warnings, result := v.validate(scheduledBackup)
		Expect(warnings).To(BeEmpty())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.onlineConfiguration"))
	})
})
