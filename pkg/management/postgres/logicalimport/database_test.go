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

package logicalimport

import (
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("databaseSnapshotter methods test", func() {
	var (
		ds   databaseSnapshotter
		fp   fakePooler
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		ds = databaseSnapshotter{
			cluster: &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
			},
		}

		db, dbMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		mock = dbMock
		fp = fakePooler{
			db: db,
		}
	})

	AfterEach(func() {
		expectationErr := mock.ExpectationsWereMet()
		Expect(expectationErr).ToNot(HaveOccurred())
	})

	Context("databaseExists testing", func() {
		var expectedQuery *sqlmock.ExpectedQuery
		BeforeEach(func() {
			expectedQuery = mock.
				ExpectQuery("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)").
				WithArgs("test")
		})

		It("should return true when the db exists", func() {
			rows := sqlmock.NewRows([]string{"*"}).AddRow(true)
			expectedQuery.WillReturnRows(rows)

			res, err := ds.databaseExists(fp, "test")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeTrue())
		})

		It("should return false when the db doesn't exists", func() {
			rows := sqlmock.NewRows([]string{"*"}).AddRow(false)
			expectedQuery.WillReturnRows(rows)

			res, err := ds.databaseExists(fp, "test")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeFalse())
		})

		It("should correctly report errors", func() {
			err := fmt.Errorf("test error")
			expectedQuery.WillReturnError(err)
			_, err = ds.databaseExists(fp, "test")
			Expect(err).To(Equal(err))
		})
	})

	Context("executePostImportQueries testing", func() {
		const createQuery = "CREATE TABLE test (id int)"
		BeforeEach(func() {
			ds.cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Import: &apiv1.Import{
						PostImportApplicationSQL: []string{createQuery},
					},
				},
			}
		})

		It("should execute the query properly", func(ctx SpecContext) {
			mock.ExpectExec(createQuery).WillReturnResult(sqlmock.NewResult(0, 0))
			err := ds.executePostImportQueries(ctx, fp, "test")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return any error encountered", func(ctx SpecContext) {
			expectedErr := fmt.Errorf("will fail")
			mock.ExpectExec(createQuery).WillReturnError(expectedErr)
			err := ds.executePostImportQueries(ctx, fp, "test")
			Expect(err).To(Equal(expectedErr))
		})
	})

	It("should run analyze", func(ctx SpecContext) {
		mock.ExpectExec("ANALYZE VERBOSE").WillReturnResult(sqlmock.NewResult(0, 0))
		err := ds.analyze(ctx, fp, []string{"test"})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("dropExtensionsFromDatabase testing", func() {
		var expectedQuery *sqlmock.ExpectedQuery

		BeforeEach(func() {
			expectedQuery = mock.ExpectQuery("SELECT extname FROM pg_catalog.pg_extension WHERE oid >= 16384")
		})

		It("should drop the user-defined extensions successfully", func(ctx SpecContext) {
			extensions := []string{"extension1", "extension2"}

			rows := sqlmock.NewRows([]string{"extname"})
			for _, ext := range extensions {
				rows.AddRow(ext)
				mock.ExpectExec("DROP EXTENSION " + pgx.Identifier{ext}.Sanitize()).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			expectedQuery.WillReturnRows(rows)

			err := ds.dropExtensionsFromDatabase(ctx, fp, "test")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should correctly handle an error when querying for extensions", func(ctx SpecContext) {
			expectedErr := fmt.Errorf("querying error")
			expectedQuery.WillReturnError(expectedErr)

			err := ds.dropExtensionsFromDatabase(ctx, fp, "test")
			Expect(err).To(Equal(expectedErr))
		})

		It("should correctly handle an error when dropping an extension", func(ctx SpecContext) {
			rows := sqlmock.NewRows([]string{"extname"}).AddRow("extension1")
			expectedQuery.WillReturnRows(rows)

			expectedErr := fmt.Errorf("dropping error")
			mock.ExpectExec(fmt.Sprintf("DROP EXTENSION %s", pgx.Identifier{"extension1"}.Sanitize())).
				WillReturnError(expectedErr)

			err := ds.dropExtensionsFromDatabase(ctx, fp, "test")
			Expect(err).ToNot(HaveOccurred()) // The function handles the error and logs it, so it should not return an error.
		})
	})

	Context("getDatabaseList testing", func() {
		const query = "SELECT datname FROM pg_catalog.pg_database d " +
			"WHERE datallowconn AND NOT datistemplate AND datallowconn AND datname != 'postgres' " +
			"ORDER BY datname"

		BeforeEach(func() {
			ds.cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Import: &apiv1.Import{},
				},
			}
		})

		It("should return the explicit database list if present", func(ctx SpecContext) {
			explicitDatabaseList := []string{"db1", "db2"}
			ds.cluster.Spec.Bootstrap.InitDB.Import.Databases = explicitDatabaseList

			dbs, err := ds.getDatabaseList(ctx, fp)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbs).To(Equal(explicitDatabaseList))
		})

		It("should query for databases if explicit list is not present", func(ctx SpecContext) {
			expectedQuery := mock.ExpectQuery(query)
			ds.cluster.Spec.Bootstrap.InitDB.Import.Databases = []string{"*"}

			queryDatabaseList := []string{"db1", "db2"}
			rows := sqlmock.NewRows([]string{"datname"})
			for _, db := range queryDatabaseList {
				rows.AddRow(db)
			}
			expectedQuery.WillReturnRows(rows)

			dbs, err := ds.getDatabaseList(ctx, fp)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbs).To(Equal(queryDatabaseList))
		})

		It("should return any error encountered when querying for databases", func(ctx SpecContext) {
			expectedErr := fmt.Errorf("querying error")
			expectedQuery := mock.ExpectQuery(query)
			ds.cluster.Spec.Bootstrap.InitDB.Import.Databases = []string{"*"}
			expectedQuery.WillReturnError(expectedErr)

			_, err := ds.getDatabaseList(ctx, fp)
			Expect(err).To(Equal(expectedErr))
		})
	})
})
