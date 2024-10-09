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
	*postgres.Instance
	db *sql.DB
}

func (f *fakeInstanceData) GetSuperUserDB() (*sql.DB, error) {
	return f.db, nil
}

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

		pgInstance := postgres.NewInstance().
			WithNamespace("default").
			WithPodName("cluster-example-1").
			WithClusterName("cluster-example")

		f := fakeInstanceData{
			Instance: pgInstance,
			db:       db,
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
			Name:      database.Name,
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

	It("marks as failed if the target Database is already being managed", func(ctx SpecContext) {
		// The Database obj currently managing "test-database"
		currentManager := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "current-manager",
				Namespace: "default",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name:  "test-database",
				Owner: "app",
			},
			Status: apiv1.DatabaseStatus{
				Ready:              true,
				ObservedGeneration: 1,
			},
		}

		// A new Database Object targeting the same "test-database"
		dbDuplicate := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "db-duplicate",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name:  "test-database",
				Owner: "app",
			},
		}

		Expect(fakeClient.Create(ctx, currentManager)).To(Succeed())
		Expect(fakeClient.Create(ctx, dbDuplicate)).To(Succeed())

		// Reconcile and get the updated object
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: dbDuplicate.Namespace,
			Name:      dbDuplicate.Name,
		}})
		Expect(err).ToNot(HaveOccurred())

		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: dbDuplicate.Namespace,
			Name:      dbDuplicate.Name,
		}, dbDuplicate)
		Expect(err).ToNot(HaveOccurred())

		expectedError := fmt.Sprintf("database %q is already managed by Database object %q",
			dbDuplicate.Spec.Name, currentManager.Name)
		Expect(dbDuplicate.Status.Ready).To(BeFalse())
		Expect(dbDuplicate.Status.Error).To(BeEquivalentTo(expectedError))
		Expect(dbDuplicate.Status.ObservedGeneration).To(BeZero())
	})
})
