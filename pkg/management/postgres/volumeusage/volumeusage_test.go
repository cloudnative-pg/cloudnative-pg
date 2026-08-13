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

package volumeusage

import (
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/diskusage"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Collect", func() {
	It("reports pgdata only when no wal volume or tablespaces exist", func() {
		root := GinkgoT().TempDir()
		pgdata := filepath.Join(root, "pgdata")
		Expect(os.MkdirAll(pgdata, 0o750)).To(Succeed())

		usages := Collect(BasePaths{
			PGData:          pgdata,
			WALVolume:       filepath.Join(root, "wal"),         // absent
			TablespacesRoot: filepath.Join(root, "tablespaces"), // absent
		})

		Expect(usages).To(HaveLen(1))
		Expect(usages[0].Name).To(Equal("pgdata"))
		Expect(usages[0].MountPoint).To(Equal(pgdata))
		Expect(usages[0].TotalBytes).To(BeNumerically(">", 0))
	})

	It("reports pgdata, wal and each tablespace when present", func() {
		root := GinkgoT().TempDir()
		pgdata := filepath.Join(root, "pgdata")
		wal := filepath.Join(root, "wal")
		tbsRoot := filepath.Join(root, "tablespaces")
		Expect(os.MkdirAll(pgdata, 0o750)).To(Succeed())
		Expect(os.MkdirAll(wal, 0o750)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tbsRoot, "atlas"), 0o750)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tbsRoot, "titan"), 0o750)).To(Succeed())

		usages := Collect(BasePaths{PGData: pgdata, WALVolume: wal, TablespacesRoot: tbsRoot})

		names := make([]string, 0, len(usages))
		mountPoints := make(map[string]string, len(usages))
		for _, u := range usages {
			names = append(names, u.Name)
			mountPoints[u.Name] = u.MountPoint
		}
		Expect(names).To(ConsistOf("pgdata", "wal", "tbs-atlas", "tbs-titan"))
		Expect(mountPoints["pgdata"]).To(Equal(pgdata))
		Expect(mountPoints["wal"]).To(Equal(wal))
		Expect(mountPoints["tbs-atlas"]).To(Equal(filepath.Join(tbsRoot, "atlas")))
		Expect(mountPoints["tbs-titan"]).To(Equal(filepath.Join(tbsRoot, "titan")))
	})

	It("still reports pgdata when tablespaces enumeration fails", func() {
		root := GinkgoT().TempDir()
		pgdata := filepath.Join(root, "pgdata")
		Expect(os.MkdirAll(pgdata, 0o750)).To(Succeed())
		// Write a regular file where the tablespaces directory is expected, so
		// GetDirectoryContent will fail when it tries to read it as a directory.
		tbsFile := filepath.Join(root, "tablespaces")
		Expect(os.WriteFile(tbsFile, []byte("x"), 0o600)).To(Succeed())

		usages := Collect(BasePaths{
			PGData:          pgdata,
			WALVolume:       filepath.Join(root, "wal"), // absent
			TablespacesRoot: tbsFile,
		})

		Expect(usages).To(HaveLen(1))
		Expect(usages[0].Name).To(Equal("pgdata"))
	})
})

var _ = Describe("SharesFilesystem", func() {
	It("detects two usages on the same filesystem", func() {
		in := []diskusage.Usage{{FilesystemID: 7}, {FilesystemID: 7}}
		Expect(SharesFilesystem(in)).To(BeTrue())
	})

	It("returns false when all filesystems differ", func() {
		in := []diskusage.Usage{{FilesystemID: 1}, {FilesystemID: 2}}
		Expect(SharesFilesystem(in)).To(BeFalse())
	})

	It("returns false for a single-element slice", func() {
		in := []diskusage.Usage{{FilesystemID: 1}}
		Expect(SharesFilesystem(in)).To(BeFalse())
	})
})
