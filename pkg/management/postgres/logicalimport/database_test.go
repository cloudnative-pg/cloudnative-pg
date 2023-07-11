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

package logicalimport

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakePooler struct {
	db *sql.DB
}

func (f fakePooler) Connection(_ string) (*sql.DB, error) {
	return f.db, nil
}

func (f fakePooler) GetDsn(dbName string) string {
	return dbName
}

func (f fakePooler) ShutdownConnections() {
}

var _ = Describe("databaseSnapshotter methods test", func() {
	var (
		ctx  context.Context
		ds   databaseSnapshotter
		fp   fakePooler
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		ctx = context.TODO()
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
				ExpectQuery("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)")
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

		It("should execute the query properly", func() {
			mock.ExpectExec(createQuery).WillReturnResult(sqlmock.NewResult(0, 0))
			err := ds.executePostImportQueries(ctx, fp, "test")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return any error encountered", func() {
			expectedErr := fmt.Errorf("will fail")
			mock.ExpectExec(createQuery).WillReturnError(expectedErr)
			err := ds.executePostImportQueries(ctx, fp, "test")
			Expect(err).To(Equal(expectedErr))
		})
	})

	It("should run analyze", func() {
		mock.ExpectExec("ANALYZE VERBOSE").WillReturnResult(sqlmock.NewResult(0, 0))
		err := ds.analyze(ctx, fp, []string{"test"})
		Expect(err).ToNot(HaveOccurred())
	})
})
