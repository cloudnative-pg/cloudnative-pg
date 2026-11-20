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
	"database/sql"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("IsBootstrapCompleted",
	func(value string, expected bool, expectError bool) {
		GinkgoT().Setenv(specs.BootstrapPendingEnvName, value)
		completed, err := IsBootstrapCompleted()
		if expectError {
			Expect(err).To(HaveOccurred())
			return
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(Equal(expected))
	},
	Entry("pending, as the operator creates the Pod", "true", false, false),
	Entry("no longer pending", "", true, false),
	Entry("explicitly not pending", "false", true, false),
	Entry("invalid value should return an error", "not-a-bool", false, true),
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

		// executeQueries brackets the batch with SET / RESET search_path
		// so user-authored DDL still resolves into "$user", public.
		mockSuperUser.ExpectExec(regexp.QuoteMeta(`SET search_path TO "$user", public`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mockSuperUser.ExpectExec("CREATE ROLE post_init_role LOGIN").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockSuperUser.ExpectExec(regexp.QuoteMeta(`RESET search_path`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := info.ConfigureNewInstance(mi)

		Expect(err).NotTo(HaveOccurred())
	})

	It("ensures that we do not create the application user if already exists", func() {
		// Expect check if application role exists - return true this time
		mockSuperUser.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1")).
			WithArgs("app_user").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		// No direct role creation expected

		mockSuperUser.ExpectExec(regexp.QuoteMeta(`SET search_path TO "$user", public`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mockSuperUser.ExpectExec("CREATE ROLE post_init_role LOGIN").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockSuperUser.ExpectExec(regexp.QuoteMeta(`RESET search_path`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		// Execute function under test
		err := info.ConfigureNewInstance(mi)

		// Verify results
		Expect(err).NotTo(HaveOccurred())
		Expect(mockSuperUser.ExpectationsWereMet()).NotTo(HaveOccurred())
	})
})

var _ = Describe("ConfigureNewInstance application database", func() {
	It("restricts the CONNECT privilege on the application database it creates", func() {
		superUserDB, mock, err := sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
		mock.MatchExpectationsInOrder(true)
		appDB, _, err := sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
		mi := &mockInstance{superUserDB: superUserDB, appDB: appDB}

		info := InitInfo{
			ApplicationUser:     "app_user",
			ApplicationDatabase: "app",
			PgData:              GinkgoT().TempDir(),
		}

		mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1")).
			WithArgs("app_user").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectQuery(regexp.QuoteMeta(
			"SELECT COUNT(*) > 0 FROM pg_catalog.pg_database WHERE datname = $1")).
			WithArgs("app").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		mock.ExpectExec(regexp.QuoteMeta(`CREATE DATABASE "app" OWNER "app_user"`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`REVOKE CONNECT ON DATABASE "app" FROM PUBLIC`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "app" TO cnpg_metrics_exporter`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		Expect(info.ConfigureNewInstance(mi)).To(Succeed())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})

var _ = Describe("executeQueries search_path bracketing", func() {
	DescribeTable("wraps the whole batch with one SET / RESET search_path",
		func(queries []string) {
			db, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = db.Close()
			}()

			mock.MatchExpectationsInOrder(true)
			mock.ExpectExec(regexp.QuoteMeta(`SET search_path TO "$user", public`)).
				WillReturnResult(sqlmock.NewResult(0, 0))
			for _, q := range queries {
				mock.ExpectExec(regexp.QuoteMeta(q)).
					WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectExec(regexp.QuoteMeta(`RESET search_path`)).
				WillReturnResult(sqlmock.NewResult(0, 0))

			info := InitInfo{}
			Expect(info.executeQueries(db, queries)).To(Succeed())
			Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
		},
		Entry("with independent statements", []string{
			"CREATE TABLE foo (id int)",
			"GRANT ALL ON foo TO app",
		}),
		// Verifies that a user-issued SET inside the batch persists for
		// subsequent statements (matches pre-pin PostgreSQL semantics).
		Entry("with a user-issued SET search_path inside", []string{
			"SET search_path TO my_schema",
			"CREATE TABLE foo (id int)",
		}),
	)

	It("is a no-op when there are no queries", func() {
		db, mock, err := sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = db.Close()
		}()

		info := InitInfo{}
		err = info.executeQueries(db, nil)

		Expect(err).NotTo(HaveOccurred())
		Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
	})
})

var _ = Describe("restricting the CONNECT privilege", func() {
	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
		mock.MatchExpectationsInOrder(true)
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("grants CONNECT to the exporter on a database", func(ctx SpecContext) {
		mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "app" TO cnpg_metrics_exporter`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		Expect(GrantMetricsExporterConnect(ctx, db, "app")).To(Succeed())
	})

	It("revokes CONNECT from PUBLIC on a database", func(ctx SpecContext) {
		mock.ExpectExec(regexp.QuoteMeta(`REVOKE CONNECT ON DATABASE "app" FROM PUBLIC`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		Expect(RestrictDatabaseConnect(ctx, db, "app")).To(Succeed())
	})

	It("revokes PUBLIC and grants the exporter in a transaction on a new database", func(ctx SpecContext) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`REVOKE CONNECT ON DATABASE "app" FROM PUBLIC`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "app" TO cnpg_metrics_exporter`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		Expect(SetupNewDatabase(ctx, db, "app")).To(Succeed())
	})

	It("rolls back the revoke when the grant on a new database fails", func(ctx SpecContext) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`REVOKE CONNECT ON DATABASE "app" FROM PUBLIC`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "app" TO cnpg_metrics_exporter`)).
			WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		Expect(SetupNewDatabase(ctx, db, "app")).To(MatchError(ContainSubstring("boom")))
	})

	It("reports the failure of the grant", func(ctx SpecContext) {
		mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "app" TO cnpg_metrics_exporter`)).
			WillReturnError(errors.New("boom"))

		Expect(GrantMetricsExporterConnect(ctx, db, "app")).To(MatchError(ContainSubstring("boom")))
	})

	It("creates the exporter and revokes PUBLIC on postgres and template1 for a new instance",
		func(ctx SpecContext) {
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) > 0 FROM pg_catalog.pg_roles WHERE rolname = $1")).
				WithArgs("cnpg_metrics_exporter").
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			mock.ExpectExec("CREATE ROLE cnpg_metrics_exporter").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "postgres" TO cnpg_metrics_exporter`)).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "template1" TO cnpg_metrics_exporter`)).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("pg_catalog.pg_authid").WithArgs("cnpg_metrics_exporter").
				WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c"}).AddRow(true, false, true))
			mock.ExpectExec("GRANT pg_monitor TO cnpg_metrics_exporter").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta(`REVOKE CONNECT ON DATABASE "postgres" FROM PUBLIC`)).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta(`REVOKE CONNECT ON DATABASE "template1" FROM PUBLIC`)).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()

			Expect(restrictInitialDatabases(ctx, db)).To(Succeed())
		})
})
