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
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scheduled backup", func() {
	scheduledBackup := &ScheduledBackup{}
	backupName := "test"

	It("properly creates a backup with no annotations", func() {
		backup := scheduledBackup.CreateBackup("test")
		Expect(backup).ToNot(BeNil())
		Expect(backup.ObjectMeta.Name).To(BeEquivalentTo(backupName))
		Expect(backup.Annotations).To(BeEmpty())
	})

	It("properly creates a backup with annotations", func() {
		annotations := make(map[string]string, 1)
		annotations["test"] = "annotations"
		scheduledBackup.Annotations = annotations
		configuration.Current.InheritedAnnotations = []string{"test"}

		backup := scheduledBackup.CreateBackup("test")
		Expect(backup).ToNot(BeNil())
		Expect(backup.ObjectMeta.Name).To(BeEquivalentTo(backupName))
		Expect(backup.Annotations).ToNot(BeEmpty())
		Expect(backup.Spec.Target).To(BeEmpty())
	})

	It("properly creates a backup with standby target", func() {
		scheduledBackup.Spec.Target = BackupTargetStandby
		backup := scheduledBackup.CreateBackup("test")
		Expect(backup).ToNot(BeNil())
		Expect(backup.ObjectMeta.Name).To(BeEquivalentTo(backupName))
		Expect(backup.Spec.Target).To(BeEquivalentTo(BackupTargetStandby))
	})

	It("properly creates a backup with primary target", func() {
		scheduledBackup.Spec.Target = BackupTargetPrimary
		backup := scheduledBackup.CreateBackup("test")
		Expect(backup).ToNot(BeNil())
		Expect(backup.ObjectMeta.Name).To(BeEquivalentTo(backupName))
		Expect(backup.Spec.Target).To(BeEquivalentTo(BackupTargetPrimary))
	})

	It("complains if online is set on a barman backup", func() {
		scheduledBackup := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Method:   BackupMethodBarmanObjectStore,
				Online:   ptr.To(true),
				Schedule: "* * * * * *",
			},
		}
		result := scheduledBackup.validate()
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.online"))
	})

	It("complains if onlineConfiguration is set on a barman backup", func() {
		scheduledBackup := &ScheduledBackup{
			Spec: ScheduledBackupSpec{
				Method:              BackupMethodBarmanObjectStore,
				OnlineConfiguration: &OnlineConfiguration{},
				Schedule:            "* * * * * *",
			},
		}
		result := scheduledBackup.validate()
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.onlineConfiguration"))
	})
})
