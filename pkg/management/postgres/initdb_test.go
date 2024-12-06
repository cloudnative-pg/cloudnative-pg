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
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EnsureTargetDirectoriesDoNotExist", func() {
	var initInfo InitInfo

	BeforeEach(func() {
		initInfo = InitInfo{
			PgData: GinkgoT().TempDir(),
			PgWal:  GinkgoT().TempDir(),
		}
		Expect(os.Create(filepath.Join(initInfo.PgData, "PG_VERSION"))).Error().NotTo(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(initInfo.PgWal, "archive_status"), 0o700)).To(Succeed())
	})

	It("should do nothing if both data and WAL directories do not exist", func(ctx SpecContext) {
		Expect(os.RemoveAll(initInfo.PgData)).Should(Succeed())
		Expect(os.RemoveAll(initInfo.PgWal)).Should(Succeed())

		err := initInfo.EnsureTargetDirectoriesDoNotExist(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Stat(initInfo.PgData)).Error().To(MatchError(os.ErrNotExist))
		Expect(os.Stat(initInfo.PgWal)).Error().To(MatchError(os.ErrNotExist))
	})

	It("should remove existing directories if pg_controldata check fails", func(ctx SpecContext) {
		err := initInfo.EnsureTargetDirectoriesDoNotExist(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Stat(initInfo.PgData)).Error().To(MatchError(os.ErrNotExist))
		Expect(os.Stat(initInfo.PgWal)).Error().To(MatchError(os.ErrNotExist))
	})

	It("should remove data directory even if WAL directory is not present", func(ctx SpecContext) {
		Expect(os.RemoveAll(initInfo.PgWal)).Should(Succeed())

		err := initInfo.EnsureTargetDirectoriesDoNotExist(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Stat(initInfo.PgData)).Error().To(MatchError(os.ErrNotExist))
		Expect(os.Stat(initInfo.PgWal)).Error().To(MatchError(os.ErrNotExist))
	})

	It("should remove WAL directory even if data directory is not present", func(ctx SpecContext) {
		Expect(os.RemoveAll(initInfo.PgData)).Should(Succeed())

		err := initInfo.EnsureTargetDirectoriesDoNotExist(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Stat(initInfo.PgData)).Error().To(MatchError(os.ErrNotExist))
		Expect(os.Stat(initInfo.PgWal)).Error().To(MatchError(os.ErrNotExist))
	})
})

var _ = Describe("renameExistingTargetDataDirectories", func() {
	var initInfo InitInfo

	BeforeEach(func() {
		initInfo = InitInfo{
			PgData: GinkgoT().TempDir(),
			PgWal:  GinkgoT().TempDir(),
		}
		Expect(os.Create(filepath.Join(initInfo.PgData, "PG_VERSION"))).Error().NotTo(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(initInfo.PgWal, "archive_status"), 0o700)).To(Succeed())
	})

	It("should rename existing data and WAL directories", func(ctx SpecContext) {
		err := initInfo.renameExistingTargetDataDirectories(ctx, true)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Stat(initInfo.PgData)).Error().To(MatchError(os.ErrNotExist))
		Expect(os.Stat(initInfo.PgWal)).Error().To(MatchError(os.ErrNotExist))

		filelist, err := filepath.Glob(initInfo.PgData + "_*")
		Expect(err).ToNot(HaveOccurred())
		Expect(filelist).To(HaveLen(1))

		filelist, err = filepath.Glob(initInfo.PgWal + "_*")
		Expect(err).ToNot(HaveOccurred())
		Expect(filelist).To(HaveLen(1))
	})

	It("should rename existing data without WAL directories", func(ctx SpecContext) {
		Expect(os.RemoveAll(initInfo.PgWal)).Should(Succeed())

		err := initInfo.renameExistingTargetDataDirectories(ctx, false)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Stat(initInfo.PgData)).Error().To(MatchError(os.ErrNotExist))
		Expect(os.Stat(initInfo.PgWal)).Error().To(MatchError(os.ErrNotExist))

		filelist, err := filepath.Glob(initInfo.PgData + "_*")
		Expect(err).ToNot(HaveOccurred())
		Expect(filelist).To(HaveLen(1))

		filelist, err = filepath.Glob(initInfo.PgWal + "_*")
		Expect(err).ToNot(HaveOccurred())
		Expect(filelist).To(BeEmpty())
	})
})
