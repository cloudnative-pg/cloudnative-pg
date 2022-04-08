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

package catalog

import (
	"time"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup catalog", func() {
	catalog := NewCatalog([]BarmanBackup{
		{
			ID:        "202101021200",
			BeginTime: time.Date(2021, 1, 2, 12, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2021, 1, 2, 12, 30, 0, 0, time.UTC),
			TimeLine:  1,
		},
		{
			ID:        "202101011200",
			BeginTime: time.Date(2021, 1, 1, 12, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2021, 1, 1, 12, 30, 0, 0, time.UTC),
			TimeLine:  1,
		},
		{
			ID:        "202101031200",
			BeginTime: time.Date(2021, 1, 3, 12, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2021, 1, 3, 12, 30, 0, 0, time.UTC),
			TimeLine:  1,
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
		recoveryTarget := &v1.RecoveryTarget{TargetTime: time.Now().Format("2006-01-02 15:04:04")}
		closestBackupInfo, err := catalog.FindClosestBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(closestBackupInfo.ID).To(Equal("202101031200"))

		recoveryTarget = &v1.RecoveryTarget{TargetTime: time.Date(2021, 1, 2, 12, 30, 0,
			0, time.UTC).Format("2006-01-02 15:04:04")}
		closestBackupInfo, err = catalog.FindClosestBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(closestBackupInfo.ID).To(Equal("202101021200"))
	})

	It("will return an empty result when the closest backup cannot be found", func() {
		recoveryTarget := &v1.RecoveryTarget{TargetTime: time.Date(2019, 1, 2, 12, 30,
			0, 0, time.UTC).Format("2006-01-02 15:04:04")}
		closestBackupInfo, err := catalog.FindClosestBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(closestBackupInfo).To(BeNil())
	})
})
