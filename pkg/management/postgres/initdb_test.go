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
	"context"
	"database/sql"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
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

var _ = Describe("configurePoolerIntegrationAfterImport", func() {
	var (
		instance *mockInstanceWithCustomPooler
		cluster  *apiv1.Cluster
		ctx      context.Context
		mockDB   sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()

		instance = &mockInstanceWithCustomPooler{}
		instance.superUserDB, mockDB, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())

		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app_db",
					},
				},
			},
			Status: apiv1.ClusterStatus{
				PoolerIntegrations: &apiv1.PoolerIntegrations{
					PgBouncerIntegration: apiv1.PgBouncerIntegrationStatus{
						Secrets: []string{"pooler-secret"},
					},
				},
			},
		}
	})

	AfterEach(func() {
		Expect(mockDB.ExpectationsWereMet()).NotTo(HaveOccurred())
	})

	Context("when cluster has no pooler integrations", func() {
		BeforeEach(func() {
			cluster.Status.PoolerIntegrations = nil
		})

		It("should return early without doing anything", func() {
			err := configurePoolerIntegrationAfterImport(ctx, instance, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when cluster has empty pooler integrations", func() {
		BeforeEach(func() {
			cluster.Status.PoolerIntegrations.PgBouncerIntegration.Secrets = []string{}
		})

		It("should return early without doing anything", func() {
			err := configurePoolerIntegrationAfterImport(ctx, instance, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when cluster has pooler integrations", func() {
		It("should configure pooler integration successfully", func() {
			appDBMock, appDBSqlMock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			importedDBMock, importedDBSqlMock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			postgresDBMock, postgresDBSqlMock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())

			instance.pooler = &customPoolerForTest{
				dbs: map[string]*sql.DB{
					"app_db":      appDBMock,
					"imported_db": importedDBMock,
					"postgres":    postgresDBMock,
				},
			}

			mockDB.ExpectExec(regexp.QuoteMeta(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cnpg_pooler_pgbouncer') THEN
				CREATE ROLE cnpg_pooler_pgbouncer WITH LOGIN;
			END IF;
		END
		$$;
	`)).WillReturnResult(sqlmock.NewResult(0, 0))

			mockDB.ExpectQuery(regexp.QuoteMeta("SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres')")).
				WillReturnRows(sqlmock.NewRows([]string{"datname"}).
					AddRow("app_db").
					AddRow("imported_db"))

			mockDB.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "postgres" TO cnpg_pooler_pgbouncer`)).
				WillReturnResult(sqlmock.NewResult(0, 0))

			postgresDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			CREATE OR REPLACE FUNCTION public.user_search(uname TEXT)
			RETURNS TABLE (usename name, passwd text)
			LANGUAGE sql SECURITY DEFINER AS
			'SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1;';
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			postgresDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			REVOKE ALL ON FUNCTION public.user_search(text) FROM public;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			postgresDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			GRANT EXECUTE ON FUNCTION public.user_search(text) TO cnpg_pooler_pgbouncer;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			mockDB.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "app_db" TO cnpg_pooler_pgbouncer`)).
				WillReturnResult(sqlmock.NewResult(0, 0))

			appDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			CREATE OR REPLACE FUNCTION public.user_search(uname TEXT)
			RETURNS TABLE (usename name, passwd text)
			LANGUAGE sql SECURITY DEFINER AS
			'SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1;';
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			appDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			REVOKE ALL ON FUNCTION public.user_search(text) FROM public;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			appDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			GRANT EXECUTE ON FUNCTION public.user_search(text) TO cnpg_pooler_pgbouncer;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			mockDB.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "imported_db" TO cnpg_pooler_pgbouncer`)).
				WillReturnResult(sqlmock.NewResult(0, 0))

			importedDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			CREATE OR REPLACE FUNCTION public.user_search(uname TEXT)
			RETURNS TABLE (usename name, passwd text)
			LANGUAGE sql SECURITY DEFINER AS
			'SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1;';
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			importedDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			REVOKE ALL ON FUNCTION public.user_search(text) FROM public;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			importedDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			GRANT EXECUTE ON FUNCTION public.user_search(text) TO cnpg_pooler_pgbouncer;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			err = configurePoolerIntegrationAfterImport(ctx, instance, cluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(appDBSqlMock.ExpectationsWereMet()).NotTo(HaveOccurred())
			Expect(importedDBSqlMock.ExpectationsWereMet()).NotTo(HaveOccurred())
			Expect(postgresDBSqlMock.ExpectationsWereMet()).NotTo(HaveOccurred())
		})

		It("should handle error when creating role fails", func() {
			mockDB.ExpectExec(regexp.QuoteMeta(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cnpg_pooler_pgbouncer') THEN
				CREATE ROLE cnpg_pooler_pgbouncer WITH LOGIN;
			END IF;
		END
		$$;
	`)).WillReturnError(errors.New("role creation failed"))

			err := configurePoolerIntegrationAfterImport(ctx, instance, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("while creating cnpg_pooler_pgbouncer role"))
		})

		It("should handle error when listing databases fails", func() {
			mockDB.ExpectExec(regexp.QuoteMeta(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cnpg_pooler_pgbouncer') THEN
				CREATE ROLE cnpg_pooler_pgbouncer WITH LOGIN;
			END IF;
		END
		$$;
	`)).WillReturnResult(sqlmock.NewResult(0, 0))

			mockDB.ExpectQuery(regexp.QuoteMeta("SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres')")).
				WillReturnError(errors.New("database listing failed"))

			err := configurePoolerIntegrationAfterImport(ctx, instance, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("while listing databases"))
		})
	})

	Context("when no application database is specified", func() {
		BeforeEach(func() {
			cluster.Spec.Bootstrap.InitDB.Database = ""
		})

		It("should only configure postgres database", func() {
			postgresDBMock, postgresDBSqlMock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())

			instance.pooler = &customPoolerForTest{
				dbs: map[string]*sql.DB{
					"postgres": postgresDBMock,
				},
			}

			mockDB.ExpectExec(regexp.QuoteMeta(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'cnpg_pooler_pgbouncer') THEN
				CREATE ROLE cnpg_pooler_pgbouncer WITH LOGIN;
			END IF;
		END
		$$;
	`)).WillReturnResult(sqlmock.NewResult(0, 0))

			mockDB.ExpectQuery(regexp.QuoteMeta("SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres')")).
				WillReturnRows(sqlmock.NewRows([]string{"datname"}))

			mockDB.ExpectExec(regexp.QuoteMeta(`GRANT CONNECT ON DATABASE "postgres" TO cnpg_pooler_pgbouncer`)).
				WillReturnResult(sqlmock.NewResult(0, 0))

			postgresDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			CREATE OR REPLACE FUNCTION public.user_search(uname TEXT)
			RETURNS TABLE (usename name, passwd text)
			LANGUAGE sql SECURITY DEFINER AS
			'SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1;';
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			postgresDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			REVOKE ALL ON FUNCTION public.user_search(text) FROM public;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			postgresDBSqlMock.ExpectExec(regexp.QuoteMeta(`
			GRANT EXECUTE ON FUNCTION public.user_search(text) TO cnpg_pooler_pgbouncer;
		`)).WillReturnResult(sqlmock.NewResult(0, 0))

			err = configurePoolerIntegrationAfterImport(ctx, instance, cluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(postgresDBSqlMock.ExpectationsWereMet()).NotTo(HaveOccurred())
		})
	})
})

type customPoolerForTest struct {
	dbs map[string]*sql.DB
}

func (c *customPoolerForTest) Connection(dbname string) (*sql.DB, error) {
	if db, exists := c.dbs[dbname]; exists {
		return db, nil
	}
	return nil, errors.New("database not found in mock")
}

func (c *customPoolerForTest) GetDsn(dbName string) string {
	return dbName
}

func (c *customPoolerForTest) ShutdownConnections() {
}

type mockInstanceWithCustomPooler struct {
	superUserDB *sql.DB
	templateDB  *sql.DB
	pooler      pool.Pooler
}

func (m *mockInstanceWithCustomPooler) GetSuperUserDB() (*sql.DB, error) {
	return m.superUserDB, nil
}

func (m *mockInstanceWithCustomPooler) GetTemplateDB() (*sql.DB, error) {
	return m.templateDB, nil
}

func (m *mockInstanceWithCustomPooler) ConnectionPool() pool.Pooler {
	return m.pooler
}
