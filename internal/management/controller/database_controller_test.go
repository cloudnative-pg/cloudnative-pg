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

package controller

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeInstanceData struct {
	instanceData
	db *sql.DB
}

func (f *fakeInstanceData) GetSuperUserDB() (*sql.DB, error) {
	return f.db, nil
}

var _ = Describe("Managed Database SQL", func() {
	var (
		dbMock   sqlmock.Sqlmock
		db       *sql.DB
		database *apiv1.Database
		r        *DatabaseReconciler
		err      error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		database = &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name: "db-one",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: "cluster-example",
				},
				Name:  "db-one",
				Owner: "app",
			},
		}
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	Context("detectDatabase", func() {
		It("returns true when it detects an existing Database", func(ctx SpecContext) {
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("1")
			dbMock.ExpectQuery(`SELECT count(*)
		FROM pg_database
		WHERE datname = $1`).WithArgs(database.Spec.Name).WillReturnRows(expectedValue)

			dbExists, err := r.detectDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbExists).To(BeTrue())
		})

		It("returns false when a Database is missing", func(ctx SpecContext) {
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(`SELECT count(*)
		FROM pg_database
		WHERE datname = $1`).WithArgs(database.Spec.Name).WillReturnRows(expectedValue)

			dbExists, err := r.detectDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbExists).To(BeFalse())
		})
	})

	Context("createDatabase", func() {
		It("should create a new Database", func(ctx SpecContext) {
			database.Spec.IsTemplate = ptr.To(true)
			database.Spec.Tablespace = "myTablespace"
			database.Spec.AllowConnections = ptr.To(true)
			database.Spec.ConnectionLimit = ptr.To(-1)

			expectedValue := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE DATABASE %s IS_TEMPLATE %v OWNER %s TABLESPACE %s "+
					"ALLOW_CONNECTIONS %v CONNECTION LIMIT %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(), *database.Spec.IsTemplate,
				pgx.Identifier{database.Spec.Owner}.Sanitize(), pgx.Identifier{database.Spec.Tablespace}.Sanitize(),
				*database.Spec.AllowConnections, *database.Spec.ConnectionLimit,
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

			err = r.createDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("patchDatabase", func() {
		It("should reconcile an existing Database", func(ctx SpecContext) {
			database.Spec.Owner = "newOwner"
			database.Spec.IsTemplate = ptr.To(true)
			database.Spec.AllowConnections = ptr.To(true)
			database.Spec.ConnectionLimit = ptr.To(-1)
			database.Spec.Tablespace = "newTablespace"

			expectedValue := sqlmock.NewResult(0, 1)

			// Mock Owner DDL
			ownerExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s OWNER TO %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				pgx.Identifier{database.Spec.Owner}.Sanitize(),
			)
			dbMock.ExpectExec(ownerExpectedQuery).WillReturnResult(expectedValue)

			// Mock IsTemplate DDL
			isTemplateExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s WITH IS_TEMPLATE %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				*database.Spec.IsTemplate,
			)
			dbMock.ExpectExec(isTemplateExpectedQuery).WillReturnResult(expectedValue)

			// Mock AllowConnections DDL
			allowConnectionsExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s WITH ALLOW_CONNECTIONS %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				*database.Spec.AllowConnections,
			)
			dbMock.ExpectExec(allowConnectionsExpectedQuery).WillReturnResult(expectedValue)

			// Mock ConnectionLimit DDL
			connectionLimitExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s WITH CONNECTION LIMIT %v",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				*database.Spec.ConnectionLimit,
			)
			dbMock.ExpectExec(connectionLimitExpectedQuery).WillReturnResult(expectedValue)

			// Mock Tablespace DDL
			tablespaceExpectedQuery := fmt.Sprintf(
				"ALTER DATABASE %s SET TABLESPACE %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
				pgx.Identifier{database.Spec.Tablespace}.Sanitize(),
			)
			dbMock.ExpectExec(tablespaceExpectedQuery).WillReturnResult(expectedValue)

			err = r.patchDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("dropDatabase", func() {
		It("should drop an existing Database", func(ctx SpecContext) {
			expectedValue := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"DROP DATABASE IF EXISTS %s",
				pgx.Identifier{database.Spec.Name}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

			err = r.dropDatabase(ctx, db, database)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("Managed Database status", func() {
	var (
		dbMock     sqlmock.Sqlmock
		db         *sql.DB
		database   *apiv1.Database
		cluster    *apiv1.Cluster
		r          *DatabaseReconciler
		fakeClient client.Client
		err        error
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "cluster-example-1",
				TargetPrimary:  "cluster-example-1",
			},
		}
		database = &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "db-one",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name:  "db-one",
				Owner: "app",
			},
		}

		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		pgInstance := &postgres.Instance{
			Namespace:   "default",
			PodName:     "cluster-example-1",
			ClusterName: "cluster-example",
		}

		f := fakeInstanceData{
			instanceData: instanceData{instance: pgInstance},
			db:           db,
		}

		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, database).
			WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Database{}).
			Build()

		r = &DatabaseReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: &f,
		}
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("database object inherits error after patching", func(ctx SpecContext) {
		// Mocking DetectDB
		expectedValue := sqlmock.NewRows([]string{""}).AddRow("1")
		dbMock.ExpectQuery(`SELECT count(*)
		FROM pg_database
		WHERE datname = $1`).WithArgs(database.Spec.Name).WillReturnRows(expectedValue)

		// Mocking Alter Database
		expectedError := fmt.Errorf("no permission")
		expectedQuery := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s",
			pgx.Identifier{database.Spec.Name}.Sanitize(),
			pgx.Identifier{database.Spec.Owner}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnError(expectedError)

		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: database.Namespace,
			Name:      database.Spec.Name,
		}})
		Expect(err).ToNot(HaveOccurred())

		var updatedDatabase apiv1.Database
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: database.Namespace,
			Name:      database.Name,
		}, &updatedDatabase)
		Expect(err).ToNot(HaveOccurred())

		Expect(updatedDatabase.Status.Ready).Should(BeFalse())
		Expect(updatedDatabase.Status.Error).Should(ContainSubstring(expectedError.Error()))
	})

	It("properly marks the status on a succeeded reconciliation", func(ctx SpecContext) {
		_, err := r.succeededReconciliation(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(database.Status.Ready).To(BeTrue())
		Expect(database.Status.Error).To(BeEmpty())
	})

	It("properly marks the status on a failed reconciliation", func(ctx SpecContext) {
		exampleError := fmt.Errorf("sample error for database %s", database.Spec.Name)

		_, err := r.failedReconciliation(ctx, database, exampleError)
		Expect(err).ToNot(HaveOccurred())
		Expect(database.Status.Ready).To(BeFalse())
		Expect(database.Status.Error).To(BeEquivalentTo(exampleError.Error()))
	})
})
