/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package catalog

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup catalog", func() {
	catalog := NewCatalog([]BarmanBackup{
		{
			ID:        "202101021200",
			BeginTime: time.Date(2021, 1, 2, 12, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2021, 1, 2, 12, 30, 0, 0, time.UTC),
		},
		{
			ID:        "202101011200",
			BeginTime: time.Date(2021, 1, 1, 12, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2021, 1, 1, 12, 30, 0, 0, time.UTC),
		},
		{
			ID:        "202101031200",
			BeginTime: time.Date(2021, 1, 3, 12, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2021, 1, 3, 12, 30, 0, 0, time.UTC),
		},
	})

	It("contains sorted data", func() {
		Expect(len(catalog.List)).To(Equal(3))
		Expect(catalog.List[0].ID).To(Equal("202101011200"))
		Expect(catalog.List[1].ID).To(Equal("202101021200"))
		Expect(catalog.List[2].ID).To(Equal("202101031200"))
	})

	It("can detect the first recoverability point", func() {
		Expect(*catalog.FirstRecoverabilityPoint()).To(
			Equal(time.Date(2021, 1, 1, 12, 30, 0, 0, time.UTC)))
	})

	It("can get the latest backupinfo", func() {
		Expect(catalog.LatestBackupInfo().ID).To(Equal("202101031200"))
	})

	It("can find the closest backup info when there is one", func() {
		Expect(catalog.FindClosestBackupInfo(time.Now()).ID).To(Equal("202101031200"))
		Expect(catalog.FindClosestBackupInfo(
			time.Date(2021, 1, 2, 12, 30, 0, 0, time.UTC)).ID).To(
			Equal("202101021200"))
	})

	It("will return an empty result when the closest backup cannot be found", func() {
		Expect(catalog.FindClosestBackupInfo(
			time.Date(2019, 1, 2, 12, 30, 0, 0, time.UTC))).To(
			BeNil())
	})
})
