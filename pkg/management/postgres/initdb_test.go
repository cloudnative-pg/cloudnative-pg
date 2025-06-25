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
	"path/filepath"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"

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

var _ = Describe("ConfigureNewInstance role creation", func() {
	var (
		info          InitInfo
		mi            *mockInstance
		mockSuperUser sqlmock.Sqlmock
		testDir       string
	)

	BeforeEach(func() {
		var err error

		testDir = path.Join(GinkgoT().TempDir(), "initdb_test")

		Expect(os.MkdirAll(testDir, 0o700)).To(Succeed())

		mi = &mockInstance{}
		mi.superUserDB, mockSuperUser, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())

		mi.appDB, _, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())

		info = InitInfo{
			ApplicationUser: "app_user",
			PostInitSQL:     []string{"CREATE ROLE post_init_role LOGIN"},
			PgData:          testDir,
		}
	})

	AfterEach(func() {
		Expect(mockSuperUser.ExpectationsWereMet()).NotTo(HaveOccurred())
	})

	It("ensures that we create the application user before postIniSQL", func() {
		// Expect check if application role exists
		mockSuperUser.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1")).
			WithArgs("app_user").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

		mockSuperUser.ExpectExec(`CREATE ROLE \"app_user\" LOGIN`).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mockSuperUser.ExpectExec("CREATE ROLE post_init_role LOGIN").
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := info.ConfigureNewInstance(mi)

		Expect(err).NotTo(HaveOccurred())
	})

	It("ensures that we do not create the application user if already exists", func() {
		// Expect check if application role exists - return true this time
		mockSuperUser.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1")).
			WithArgs("app_user").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		// No direct role creation expected

		mockSuperUser.ExpectExec("CREATE ROLE post_init_role LOGIN").
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Execute function under test
		err := info.ConfigureNewInstance(mi)

		// Verify results
		Expect(err).NotTo(HaveOccurred())
		Expect(mockSuperUser.ExpectationsWereMet()).NotTo(HaveOccurred())
	})
})
