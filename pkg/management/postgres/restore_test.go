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
			err := fileutils.EnsureDirectoryExist(pgWal)
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
			Expect(len(fileNames)).To(Equal(len(fileNameAndContent)))
		})

		By("executing the restore custom wal dir function", func() {
			chg, err := initInfo.restoreCustomWalDir(context.TODO())
			Expect(err).ToNot(HaveOccurred())
			Expect(chg).To(BeTrue())
		})

		By("ensuring that the content was migrated", func() {
			files, err := fileutils.GetDirectoryContent(initInfo.PgWal)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(files)).To(Equal(len(fileNameAndContent)))
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

		err := fileutils.EnsureDirectoryExist(newPgWal)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.EnsureDirectoryExist(pgData)
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
