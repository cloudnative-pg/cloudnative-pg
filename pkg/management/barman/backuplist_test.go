/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package barman

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	barmanCloudListOutput = `{
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
)

var _ = Describe("barman-cloud-backup-list parsing", func() {
	It("must parse a correct output", func() {
		result, err := ParseBarmanCloudBackupList(barmanCloudListOutput)
		Expect(err).To(BeNil())
		Expect(len(result.List)).To(Equal(2))
		Expect(result.List[0].ID).To(Equal("20201020T115231"))
		Expect(result.List[0].SystemID).To(Equal("6885668674852188181"))
		Expect(result.List[0].BeginTimeString).To(Equal("Tue Oct 20 11:52:31 2020"))
		Expect(result.List[0].EndTimeString).To(Equal("Tue Oct 20 11:52:34 2020"))
	})

	It("must extract the latest backup id", func() {
		result, err := ParseBarmanCloudBackupList(barmanCloudListOutput)
		Expect(err).To(BeNil())
		Expect(result.LatestBackupInfo().ID).To(Equal("20201020T115231"))
	})
})
