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

package run

import (
	"encoding/base64"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/bootstrap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("bootstrap flag parsing", func() {
	Describe("instruction", func() {
		It("reports no bootstrap when the mode is empty", func() {
			opts := bootstrapOptions{}
			_, requested := opts.instruction()
			Expect(requested).To(BeFalse())
		})

		It("maps the mode and immediate flag", func() {
			opts := bootstrapOptions{mode: "restoresnapshot", immediate: true}
			instruction, requested := opts.instruction()
			Expect(requested).To(BeTrue())
			Expect(instruction.Mode).To(Equal(bootstrap.ModeRestoreSnapshot))
			Expect(instruction.Immediate).To(BeTrue())
		})
	})

	Describe("initInfo", func() {
		It("carries the instance identity and the mode-specific fields", func() {
			opts := bootstrapOptions{
				mode:                             "initdb",
				pgWal:                            "/var/pgwal",
				parentNode:                       "cluster-1",
				appDBName:                        "app",
				appUser:                          "appuser",
				initDBFlags:                      "--data-checksums --encoding UTF8",
				postInitSQL:                      "SELECT 1",
				postInitApplicationSQL:           "SELECT 2",
				postInitTemplateSQL:              "SELECT 3",
				postInitSQLRefsFolder:            "/refs/sql",
				postInitApplicationSQLRefsFolder: "/refs/app",
				postInitTemplateSQLRefsFolder:    "/refs/tpl",
			}

			info, err := opts.initInfo("/var/pgdata", "cluster-2", "cluster", "default")
			Expect(err).ToNot(HaveOccurred())

			Expect(info.PgData).To(Equal("/var/pgdata"))
			Expect(info.PgWal).To(Equal("/var/pgwal"))
			Expect(info.PodName).To(Equal("cluster-2"))
			Expect(info.ClusterName).To(Equal("cluster"))
			Expect(info.Namespace).To(Equal("default"))
			Expect(info.ParentNode).To(Equal("cluster-1"))
			Expect(info.ApplicationDatabase).To(Equal("app"))
			Expect(info.ApplicationUser).To(Equal("appuser"))
			Expect(info.InitDBOptions).To(Equal([]string{"--data-checksums", "--encoding", "UTF8"}))
			Expect(info.PostInitSQL).To(Equal([]string{"SELECT", "1"}))
			Expect(info.PostInitApplicationSQL).To(Equal([]string{"SELECT", "2"}))
			Expect(info.PostInitTemplateSQL).To(Equal([]string{"SELECT", "3"}))
			Expect(info.PostInitSQLRefsFolder).To(Equal("/refs/sql"))
			Expect(info.PostInitApplicationSQLRefsFolder).To(Equal("/refs/app"))
			Expect(info.PostInitTemplateSQLRefsFolder).To(Equal("/refs/tpl"))
		})

		It("base64-decodes the backup label and tablespace map", func() {
			opts := bootstrapOptions{
				mode:          "restoresnapshot",
				backupLabel:   base64.StdEncoding.EncodeToString([]byte("the-backup-label")),
				tablespaceMap: base64.StdEncoding.EncodeToString([]byte("the-tablespace-map")),
			}

			info, err := opts.initInfo("/var/pgdata", "pod", "cluster", "default")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(info.BackupLabelFile)).To(Equal("the-backup-label"))
			Expect(string(info.TablespaceMapFile)).To(Equal("the-tablespace-map"))
		})

		It("leaves the backup label and tablespace map empty when the flags are unset", func() {
			opts := bootstrapOptions{mode: "restore"}
			info, err := opts.initInfo("/var/pgdata", "pod", "cluster", "default")
			Expect(err).ToNot(HaveOccurred())
			Expect(info.BackupLabelFile).To(BeNil())
			Expect(info.TablespaceMapFile).To(BeNil())
		})

		It("fails on an invalid base64 backup label", func() {
			opts := bootstrapOptions{mode: "restoresnapshot", backupLabel: "not-base64!!!"}
			_, err := opts.initInfo("/var/pgdata", "pod", "cluster", "default")
			Expect(err).To(HaveOccurred())
		})

		It("fails on unbalanced quoting in the initdb flags", func() {
			opts := bootstrapOptions{mode: "initdb", initDBFlags: `--encoding "UTF8`}
			_, err := opts.initInfo("/var/pgdata", "pod", "cluster", "default")
			Expect(err).To(HaveOccurred())
		})
	})
})
