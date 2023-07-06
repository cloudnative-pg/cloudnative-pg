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

package postgres

import (
	"context"
	"os"
	"path"

	"github.com/thoas/go-funk"
	"k8s.io/utils/strings/slices"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing restore InitInfo methods", func() {
	tempDir, err := os.MkdirTemp("", "restore")
	Expect(err).ToNot(HaveOccurred())

	pgData := path.Join(tempDir, "postgres", "data", "pgdata")
	pgWal := path.Join(pgData, "pg_wal")
	newPgWal := path.Join(tempDir, "postgres", "wal", "pg_wal")

	AfterEach(func() {
		_ = fileutils.RemoveDirectoryContent(tempDir)
		_ = fileutils.RemoveFile(tempDir)
	})

	It("should correctly restore a custom PgWal folder without data", func() {
		initInfo := InitInfo{
			PgData: pgData,
			PgWal:  newPgWal,
		}

		chg, err := initInfo.restoreCustomWalDir(context.TODO())
		Expect(err).ToNot(HaveOccurred())
		Expect(chg).To(BeTrue())

		exists, err := fileutils.FileExists(newPgWal)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("should correctly migrate an existing wal folder to the new one", func() {
		initInfo := InitInfo{
			PgData: pgData,
			PgWal:  newPgWal,
		}

		fileNameAndContent := map[string]string{
			"000000010000000000000001":                 funk.RandomString(12),
			"000000010000000000000002":                 funk.RandomString(12),
			"000000010000000000000003":                 funk.RandomString(12),
			"000000010000000000000004":                 funk.RandomString(12),
			"000000010000000000000004.00000028.backup": funk.RandomString(12),
		}

		By("creating and seeding the pg_wal directory", func() {
			err := fileutils.EnsureDirectoryExists(pgWal)
			Expect(err).ToNot(HaveOccurred())
		})
		By("seeding the directory with random content", func() {
			for name, content := range fileNameAndContent {
				filePath := path.Join(pgWal, name)
				chg, err := fileutils.WriteStringToFile(filePath, content)
				Expect(err).ToNot(HaveOccurred())
				Expect(chg).To(BeTrue())
			}

			fileNames, err := fileutils.GetDirectoryContent(pgWal)

			GinkgoWriter.Println(fileNames)
			Expect(err).ToNot(HaveOccurred())
			Expect(fileNames).To(HaveLen(len(fileNameAndContent)))
		})

		By("executing the restore custom wal dir function", func() {
			chg, err := initInfo.restoreCustomWalDir(context.TODO())
			Expect(err).ToNot(HaveOccurred())
			Expect(chg).To(BeTrue())
		})

		By("ensuring that the content was migrated", func() {
			files, err := fileutils.GetDirectoryContent(initInfo.PgWal)
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(HaveLen(len(fileNameAndContent)))
			GinkgoWriter.Println(files)

			for name, expectedContent := range fileNameAndContent {
				contained := slices.Contains(files, name)
				Expect(contained).To(BeTrue())
				contentInsideFile, err := fileutils.ReadFile(path.Join(newPgWal, name))
				Expect(err).ToNot(HaveOccurred())
				Expect(string(contentInsideFile)).To(Equal(expectedContent))
			}
		})

		By("ensuring that the new pg_wal has the symlink", func() {
			link, err := os.Readlink(pgWal)
			Expect(err).ToNot(HaveOccurred())
			Expect(link).To(Equal(newPgWal))
		})
	})

	It("should not do any changes if the symlink is already present", func() {
		initInfo := InitInfo{
			PgData: pgData,
			PgWal:  newPgWal,
		}

		err := fileutils.EnsureDirectoryExists(newPgWal)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.EnsureDirectoryExists(pgData)
		Expect(err).ToNot(HaveOccurred())

		err = os.Symlink(newPgWal, pgWal)
		Expect(err).ToNot(HaveOccurred())

		chg, err := initInfo.restoreCustomWalDir(context.TODO())
		Expect(err).ToNot(HaveOccurred())
		Expect(chg).To(BeFalse())
	})

	It("should not do any changes if pgWal is not set", func() {
		initInfo := InitInfo{
			PgData: pgData,
		}
		chg, err := initInfo.restoreCustomWalDir(context.TODO())
		Expect(err).ToNot(HaveOccurred())
		Expect(chg).To(BeFalse())
	})
})

var _ = Describe("pg_controldata value parsing", func() {
	const pgControlDataExample = `pg_control version number:            1300
Catalog version number:               202209061
Database system identifier:           7252663423934898207
Database cluster state:               in production
pg_control last modified:             Thu 06 Jul 2023 11:23:48 AM UTC
Latest checkpoint location:           0/6000060
Latest checkpoint's REDO location:    0/50515B8
Latest checkpoint's REDO WAL file:    000000010000000000000005
Latest checkpoint's TimeLineID:       1
Latest checkpoint's PrevTimeLineID:   1
Latest checkpoint's full_page_writes: on
Latest checkpoint's NextXID:          0:740
Latest checkpoint's NextOID:          24578
Latest checkpoint's NextMultiXactId:  1
Latest checkpoint's NextMultiOffset:  0
Latest checkpoint's oldestXID:        716
Latest checkpoint's oldestXID's DB:   1
Latest checkpoint's oldestActiveXID:  740
Latest checkpoint's oldestMultiXid:   1
Latest checkpoint's oldestMulti's DB: 1
Latest checkpoint's oldestCommitTsXid:0
Latest checkpoint's newestCommitTsXid:0
Time of latest checkpoint:            Thu 06 Jul 2023 11:23:44 AM UTC
Fake LSN counter for unlogged rels:   0/3E8
Minimum recovery ending location:     0/0
Min recovery ending loc's timeline:   0
Backup start location:                0/0
Backup end location:                  0/0
End-of-backup record required:        no
wal_level setting:                    logical
wal_log_hints setting:                on
max_connections setting:              100
max_worker_processes setting:         32
max_wal_senders setting:              10
max_prepared_xacts setting:           0
max_locks_per_xact setting:           64
track_commit_timestamp setting:       off
Maximum data alignment:               8
Database block size:                  8192
Blocks per segment of large relation: 131072
WAL block size:                       8192
Bytes per WAL segment:                16777216
Maximum length of identifiers:        64
Maximum columns in an index:          32
Maximum size of a TOAST chunk:        1996
Size of a large-object chunk:         2048
Date/time type storage:               64-bit integers
Float8 argument passing:              by value
Data page checksum version:           0
Mock authentication nonce:            078c6dcdd810422ac9966607e8a18daabdf6cf29cea777da3d41b66705a6ba37
`

	It("should be able to fetch 'Latest checkpoint's REDO WAL file'", func() {
		res, err := getValueFromPGControlData(pgControlDataExample, latestCheckpointRedoWAL)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal("000000010000000000000005"))
	})

	It("should return an error when parsing a non existent key", func() {
		res, err := getValueFromPGControlData(pgControlDataExample, "non-existent")
		Expect(res).To(Equal(""))
		Expect(err).To(HaveOccurred())
	})
})
