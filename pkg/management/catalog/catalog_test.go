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

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

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
		closestBackupInfo, err := catalog.FindBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(closestBackupInfo.ID).To(Equal("202101031200"))

		recoveryTarget = &v1.RecoveryTarget{TargetTime: time.Date(2021, 1, 2, 12, 30, 0,
			0, time.UTC).Format("2006-01-02 15:04:04")}
		closestBackupInfo, err = catalog.FindBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(closestBackupInfo.ID).To(Equal("202101021200"))
	})

	It("will return an empty result when the closest backup cannot be found", func() {
		recoveryTarget := &v1.RecoveryTarget{TargetTime: time.Date(2019, 1, 2, 12, 30,
			0, 0, time.UTC).Format("2006-01-02 15:04:04")}
		closestBackupInfo, err := catalog.FindBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(closestBackupInfo).To(BeNil())
	})

	It("can find the backup info when BackupID is provided", func() {
		recoveryTarget := &v1.RecoveryTarget{TargetName: "recovery_point_1", BackupID: "202101021200"}
		BackupInfo, err := catalog.FindBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(BackupInfo.ID).To(Equal("202101021200"))

		trueVal := true
		recoveryTarget = &v1.RecoveryTarget{TargetImmediate: &trueVal, BackupID: "202101011200"}
		BackupInfo, err = catalog.FindBackupInfo(recoveryTarget)
		Expect(err).ToNot(HaveOccurred())
		Expect(BackupInfo.ID).To(Equal("202101011200"))
	})
})

var _ = Describe("barman-cloud-backup-list parsing", func() {
	const barmanCloudListOutput = `{
  "backups_list": [
    {
      "backup_label": "'START WAL LOCATION:[...]",
      "begin_offset": 40,
      "begin_time": "Tue Oct 20 11:52:31 2020",
      "begin_wal": "000000010000000000000006",
      "begin_xlog": "0/6000028",
      "config_file": "/var/lib/postgresql/data/pgdata/postgresql.conf",
      "copy_stats": {
        "total_time": 4.285494,
        "number_of_workers": 2,
        "analysis_time": 0,
        "analysis_time_per_item": {
          "data": 0
        },
        "copy_time_per_item": {
          "data": 1.368199
        },
        "serialized_copy_time_per_item": {
          "data": 0.433392
        },
        "copy_time": 1.368199,
        "serialized_copy_time": 0.433392
      },
      "deduplicated_size": null,
      "end_offset": 312,
      "end_time": "Tue Oct 20 11:52:34 2020",
      "end_wal": "000000010000000000000006",
      "end_xlog": "0/6000138",
      "error": null,
      "hba_file": "/var/lib/postgresql/data/pgdata/pg_hba.conf",
      "ident_file": "/var/lib/postgresql/data/pgdata/pg_ident.conf",
      "included_files": [
        "/var/lib/postgresql/data/pgdata/custom.conf"
      ],
      "mode": null,
      "pgdata": "/var/lib/postgresql/data/pgdata",
      "server_name": "cloud",
      "size": null,
      "status": "DONE",
      "systemid": "6885668674852188181",
      "tablespaces": null,
      "timeline": 1,
      "version": 120004,
      "xlog_segment_size": 16777216,
      "backup_id": "20201020T115231"
    },
	{
      "backup_id": "20191020T115231"
	}
  ]
}`

	It("must parse a correct output", func() {
		result, err := NewCatalogFromBarmanCloudBackupList(barmanCloudListOutput)
		Expect(err).To(BeNil())
		Expect(len(result.List)).To(Equal(2))
		Expect(result.List[0].ID).To(Equal("20201020T115231"))
		Expect(result.List[0].SystemID).To(Equal("6885668674852188181"))
		Expect(result.List[0].BeginTimeString).To(Equal("Tue Oct 20 11:52:31 2020"))
		Expect(result.List[0].EndTimeString).To(Equal("Tue Oct 20 11:52:34 2020"))
	})

	It("must extract the latest backup id", func() {
		result, err := NewCatalogFromBarmanCloudBackupList(barmanCloudListOutput)
		Expect(err).To(BeNil())
		Expect(result.LatestBackupInfo().ID).To(Equal("20201020T115231"))
	})
})

var _ = Describe("barman-cloud-backup-show parsing", func() {
	const barmanCloudShowOutput = `{
		"cloud":{
            "backup_label": null,
            "begin_offset": 40,
            "begin_time": "Tue Jan 19 03:14:08 2038",
            "begin_wal": "000000010000000000000002",
            "begin_xlog": "0/2000028",
            "compression": null,
            "config_file": "/pgdata/location/postgresql.conf",
            "copy_stats": null,
            "deduplicated_size": null,
            "end_offset": 184,
            "end_time": "Tue Jan 19 04:14:08 2038",
            "end_wal": "000000010000000000000004",
            "end_xlog": "0/20000B8",
            "error": null,
            "hba_file": "/pgdata/location/pg_hba.conf",
            "ident_file": "/pgdata/location/pg_ident.conf",
            "included_files": null,
            "mode": "concurrent",
            "pgdata": "/pgdata/location",
            "server_name": "main",
            "size": null,
            "snapshots_info": {
                "provider": "gcp",
                "provider_info": {
                    "project": "test_project"
                },
                "snapshots": [
                    {
                        "mount": {
                            "mount_options": "rw,noatime",
                            "mount_point": "/opt/disk0"
                        },
                        "provider": {
                            "device_name": "dev0",
                            "snapshot_name": "snapshot0",
                            "snapshot_project": "test_project"
                        }
                    },
                    {
                        "mount": {
                            "mount_options": "rw",
                            "mount_point": "/opt/disk1"
                        },
                        "provider": {
                            "device_name": "dev1",
                            "snapshot_name": "snapshot1",
                            "snapshot_project": "test_project"
                        }
                    }
                ]
            },
            "status": "DONE",
            "systemid": "6885668674852188181",
            "tablespaces": [
                ["tbs1", 16387, "/fake/location"],
                ["tbs2", 16405, "/another/location"]
            ],
            "timeline": 1,
            "version": 150000,
            "xlog_segment_size": 16777216,
            "backup_id": "20201020T115231"
        }
}`

	It("must parse a correct output", func() {
		result, err := NewBackupFromBarmanCloudBackupShow(barmanCloudShowOutput)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.ID).To(Equal("20201020T115231"))
		Expect(result.SystemID).To(Equal("6885668674852188181"))
		Expect(result.BeginTimeString).To(Equal("Tue Jan 19 03:14:08 2038"))
		Expect(result.EndTimeString).To(Equal("Tue Jan 19 04:14:08 2038"))
	})
})
