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
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"

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
						maxConnectionsParameter:     "200",
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
		Expect(enforcedParamsInPGData[maxConnectionsParameter]).To(Equal(200))
		Expect(enforcedParamsInPGData["max_wal_senders"]).To(Equal(20))
		Expect(enforcedParamsInPGData["max_worker_processes"]).To(Equal(18))
		Expect(enforcedParamsInPGData["max_prepared_transactions"]).To(Equal(50))
	})

	It("report error if user given one in incorrect value in the cluster", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						maxConnectionsParameter:     "200s",
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
						maxConnectionsParameter: "200",
						"wal_sender_timeout":    "10min",
					},
				},
			},
		}
		enforcedParamsInPGData, err := LoadEnforcedParametersFromCluster(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(enforcedParamsInPGData).To(HaveLen(1))
		Expect(enforcedParamsInPGData[maxConnectionsParameter]).To(Equal(200))
	})
})

var _ = Describe("getRestoreWalConfig", func() {
	It("delegates WAL recovery to the instance manager wal-restore command", func() {
		out := getRestoreWalConfig()

		// restore_command must invoke the in-tree controller rather than
		// barman-cloud directly: the recovery source store and credentials are
		// provided through the local webserver cache instead of being embedded
		// in (and shell-quoted into) the command line.
		Expect(out).To(ContainSubstring("/controller/manager wal-restore"))
		Expect(out).To(ContainSubstring("%f"))
		Expect(out).To(ContainSubstring("%p"))
		Expect(out).To(ContainSubstring("recovery_target_action"))
		Expect(out).To(ContainSubstring("promote"))
		Expect(out).ToNot(ContainSubstring("barman-cloud-wal-restore"))
	})
})

var _ = Describe("setupBootstrapWALRestoreCache", func() {
	AfterEach(func() {
		cache.Delete(cache.WALRestoreKey)
		cache.Delete(cache.WALRestoreConfigKey)
	})

	backup := func() *apiv1.Backup {
		return &apiv1.Backup{
			Status: apiv1.BackupStatus{
				BarmanCredentials: apiv1.BarmanCredentials{AWS: &apiv1.S3Credentials{}},
				EndpointURL:       "https://source-endpoint",
				DestinationPath:   "s3://source/path",
				ServerName:        "source-server",
			},
		}
	}

	loadCachedStore := func() *apiv1.BarmanObjectStoreConfiguration {
		cached, err := cache.Load(cache.WALRestoreConfigKey)
		Expect(err).ToNot(HaveOccurred())
		store, ok := cached.(*apiv1.BarmanObjectStoreConfiguration)
		Expect(ok).To(BeTrue())
		return store
	}

	It("caches the recovery source store and credentials for a recovery.backup reference", func() {
		// recovery.backup: no Source, so no Wal config is available.
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Backup: &apiv1.BackupSource{
							LocalObjectReference: apiv1.LocalObjectReference{Name: "a-backup"},
						},
					},
				},
			},
		}
		env := []string{"AWS_ACCESS_KEY_ID=source-key"}

		setupBootstrapWALRestoreCache(cluster, backup(), env)

		cachedEnv, err := cache.LoadEnv(cache.WALRestoreKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(cachedEnv).To(Equal(env))

		// The cached store must target the recovery SOURCE, not the cluster's own
		// backup store, and carries no Wal config for a recovery.backup reference.
		store := loadCachedStore()
		Expect(store.DestinationPath).To(Equal("s3://source/path"))
		Expect(store.ServerName).To(Equal("source-server"))
		Expect(store.EndpointURL).To(Equal("https://source-endpoint"))
		Expect(store.Wal).To(BeNil())
	})

	It("carries the source store's Wal config for a recovery.source", func() {
		// recovery.source: the source store's Wal config lives in the external
		// cluster definition and must be carried over, just like the plugin does.
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{Source: "origin"},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "origin",
						BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
							Wal: &apiv1.WalBackupConfiguration{
								MaxParallel:                  2,
								RestoreAdditionalCommandArgs: []string{"--read-timeout=60"},
							},
						},
					},
				},
			},
		}

		setupBootstrapWALRestoreCache(cluster, backup(), []string{"X=y"})

		store := loadCachedStore()
		Expect(store.Wal).ToNot(BeNil())
		Expect(store.Wal.MaxParallel).To(Equal(2))
		Expect(store.Wal.RestoreAdditionalCommandArgs).To(ContainElement("--read-timeout=60"))
	})
})
