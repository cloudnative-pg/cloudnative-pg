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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scheduled backup", func() {
	var scheduledBackup *ScheduledBackup
	backupName := "test"

	BeforeEach(func() {
		scheduledBackup = &ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: make(map[string]string),
			},
		}
	})

	It("properly creates a backup with no annotations", func() {
		backup := scheduledBackup.CreateBackup("test")
		Expect(backup).ToNot(BeNil())
		Expect(backup.ObjectMeta.Name).To(BeEquivalentTo(backupName))
		Expect(backup.Annotations).To(BeEmpty())
	})

	It("should always inherit volumeSnapshotDeadline while creating a backup", func() {
		scheduledBackup.Annotations[utils.BackupVolumeSnapshotDeadlineAnnotationName] = "20"
		backup := scheduledBackup.CreateBackup("test")
		Expect(backup).ToNot(BeNil())
		Expect(backup.ObjectMeta.Name).To(BeEquivalentTo(backupName))
		Expect(backup.Annotations[utils.BackupVolumeSnapshotDeadlineAnnotationName]).To(BeEquivalentTo("20"))
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
})
