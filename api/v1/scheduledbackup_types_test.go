/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
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
