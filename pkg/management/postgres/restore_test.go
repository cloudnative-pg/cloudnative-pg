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

package postgres

import (
	"os"
	"path"
	"slices"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/thoas/go-funk"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing restore InitInfo methods", func() {
	tempDir, err := os.MkdirTemp("", "restore")
	Expect(err).ToNot(HaveOccurred())

	pgData := path.Join(tempDir, "postgres", "data", "pgdata")
	pgWal := path.Join(pgData, pgWalDirectory)
	newPgWal := path.Join(tempDir, "postgres", "wal", pgWalDirectory)

	AfterEach(func() {
		_ = fileutils.RemoveDirectoryContent(tempDir)
		_ = fileutils.RemoveFile(tempDir)
	})

	It("should correctly restore a custom PgWal folder without data", func(ctx SpecContext) {
		initInfo := InitInfo{
			PgData: pgData,
			PgWal:  newPgWal,
		}

		chg, err := initInfo.restoreCustomWalDir(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(chg).To(BeTrue())

		exists, err := fileutils.FileExists(newPgWal)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("should correctly migrate an existing wal folder to the new one", func(ctx SpecContext) {
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
			chg, err := initInfo.restoreCustomWalDir(ctx)
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

	It("should not do any changes if the symlink is already present", func(ctx SpecContext) {
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

		chg, err := initInfo.restoreCustomWalDir(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(chg).To(BeFalse())
	})

	It("should not do any changes if pgWal is not set", func(ctx SpecContext) {
		initInfo := InitInfo{
			PgData: pgData,
		}
		chg, err := initInfo.restoreCustomWalDir(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(chg).To(BeFalse())
	})

	It("should parse enforced params from cluster", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_connections":           "200",
						"max_wal_senders":           "20",
						"max_worker_processes":      "18",
						"max_prepared_transactions": "50",
					},
				},
			},
		}
		enforcedParamsInPGData, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(enforcedParamsInPGData).To(HaveLen(4))
		Expect(enforcedParamsInPGData["max_connections"]).To(Equal(200))
		Expect(enforcedParamsInPGData["max_wal_senders"]).To(Equal(20))
		Expect(enforcedParamsInPGData["max_worker_processes"]).To(Equal(18))
		Expect(enforcedParamsInPGData["max_prepared_transactions"]).To(Equal(50))
	})

	It("report error if user given one in incorrect value in the cluster", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_connections":           "200s",
						"max_wal_senders":           "20",
						"max_worker_processes":      "18",
						"max_prepared_transactions": "50",
					},
				},
			},
		}
		_, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).To(HaveOccurred())
	})

	It("ignore the non-enforced params user give", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_connections":    "200",
						"wal_sender_timeout": "10min",
					},
				},
			},
		}
		enforcedParamsInPGData, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(enforcedParamsInPGData).To(HaveLen(1))
		Expect(enforcedParamsInPGData["max_connections"]).To(Equal(200))
	})
})
