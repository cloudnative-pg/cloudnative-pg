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

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
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
	})
})
