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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	publicationDetectionQuery = `SELECT count(*)
		FROM pg_publication
		WHERE pubname = $1`
)

var _ = Describe("Managed publication controller tests", func() {
	var (
		dbMock      sqlmock.Sqlmock
		db          *sql.DB
		publication *apiv1.Publication
		cluster     *apiv1.Cluster
		r           *PublicationReconciler
		fakeClient  client.Client
		err         error
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
		publication = &apiv1.Publication{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "db-one",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.PublicationSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				ReclaimPolicy: apiv1.PublicationReclaimDelete,
				Name:          "pub-all",
				DBName:        "app",
				Target: apiv1.PublicationTarget{
					AllTables: true,
					Objects: []apiv1.PublicationTargetObject{
						{TablesInSchema: "public"},
					},
				},
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
			WithObjects(cluster, publication).
			WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Publication{}).
			Build()

		r = &PublicationReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: &f,
		}
		r.finalizerReconciler = newFinalizerReconciler(
			fakeClient,
			utils.PublicationFinalizerName,
			r.evaluateDropPublication,
		)
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("adds finalizer and sets status ready on success", func(ctx SpecContext) {
		Expect(publication.Finalizers).To(BeEmpty())

		// Mocking Detect
		noHits := sqlmock.NewRows([]string{""}).AddRow("0")
		dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
			WillReturnRows(noHits)

		// Mocking CreatePublication
		expectedCreate := sqlmock.NewResult(0, 1)
		expectedQuery := fmt.Sprintf(
			"CREATE PUBLICATION %s FOR ALL TABLES",
			pgx.Identifier{publication.Spec.Name}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

		// Reconcile and get the updated object
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}})
		Expect(err).ToNot(HaveOccurred())

		var updatedPublication apiv1.Publication
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}, &updatedPublication)
		Expect(err).ToNot(HaveOccurred())

		Expect(updatedPublication.Status.Applied).Should(HaveValue(BeTrue()))
		Expect(updatedPublication.Status.Message).Should(BeEmpty())
		Expect(updatedPublication.Finalizers).NotTo(BeEmpty())
	})

	It("publication object inherits error after patching", func(ctx SpecContext) {
		// Mocking Detect
		oneHit := sqlmock.NewRows([]string{""}).AddRow("1")
		dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
			WillReturnRows(oneHit)

		// Mocking Alter Publication
		expectedError := fmt.Errorf("no permission")
		expectedQuery := fmt.Sprintf("ALTER PUBLICATION %s SET TABLES IN SCHEMA \"public\"",
			pgx.Identifier{publication.Spec.Name}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnError(expectedError)

		// Reconcile and get the updated object
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}})
		Expect(err).ToNot(HaveOccurred())

		var updatedPublication apiv1.Publication
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}, &updatedPublication)
		Expect(err).ToNot(HaveOccurred())

		Expect(updatedPublication.Status.Applied).Should(HaveValue(BeFalse()))
		Expect(updatedPublication.Status.Message).Should(ContainSubstring(expectedError.Error()))
	})

	It("on deletion it removes finalizers and drops publication", func(ctx SpecContext) {
		Expect(publication.Finalizers).To(BeEmpty())

		// Mocking Detect
		noHits := sqlmock.NewRows([]string{""}).AddRow("0")
		dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
			WillReturnRows(noHits)

		// Mocking CreatePublication
		expectedCreate := sqlmock.NewResult(0, 1)
		expectedQuery := fmt.Sprintf(
			"CREATE PUBLICATION %s FOR ALL TABLES",
			pgx.Identifier{publication.Spec.Name}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

		// Reconcile and get the updated object
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}})
		Expect(err).ToNot(HaveOccurred())

		var updatedPublication apiv1.Publication
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}, &updatedPublication)
		Expect(err).ToNot(HaveOccurred())

		Expect(updatedPublication.Status.Applied).Should(HaveValue(BeTrue()))
		Expect(updatedPublication.Status.Message).Should(BeEmpty())
		Expect(updatedPublication.Finalizers).NotTo(BeEmpty())

		// the next 3 lines are a hacky bit to make sure the next reconciler
		// call doesn't skip on account of Generation == ObservedGeneration.
		// See fake.Client known issues with `Generation`
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
		currentPublication := updatedPublication.DeepCopy()
		updatedPublication.Status.ObservedGeneration = 2
		Expect(fakeClient.Status().Patch(ctx, &updatedPublication, client.MergeFrom(currentPublication))).To(Succeed())

		// We now look at the behavior when we delete the Publication object
		Expect(fakeClient.Delete(ctx, publication)).To(Succeed())

		// the Publication object is Deleted, but its finalizer prevents removal from
		// the API
		var fadingPublication apiv1.Publication
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}, &fadingPublication)
		Expect(err).ToNot(HaveOccurred())
		Expect(fadingPublication.DeletionTimestamp).NotTo(BeZero())
		Expect(fadingPublication.Finalizers).NotTo(BeEmpty())

		// Mocking Drop Publication
		expectedDrop := fmt.Sprintf("DROP PUBLICATION IF EXISTS %s",
			pgx.Identifier{publication.Spec.Name}.Sanitize(),
		)
		dbMock.ExpectExec(expectedDrop).WillReturnResult(sqlmock.NewResult(0, 1))

		// Reconcile and get the updated object
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}})
		Expect(err).ToNot(HaveOccurred())

		var finalPublication apiv1.Publication
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: publication.Namespace,
			Name:      publication.Name,
		}, &finalPublication)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	// It("fails reconciliation if cluster isn't found (deleted cluster)", func(ctx SpecContext) {
	// 	// since the fakeClient has the `cluster-example` cluster, let's reference
	// 	// another cluster `cluster-other` that is not found by the fakeClient
	// 	pgInstance := postgres.NewInstance().
	// 		WithNamespace("default").
	// 		WithPodName("cluster-other-1").
	// 		WithClusterName("cluster-other")

	// 	f := fakeInstanceData{
	// 		Instance: pgInstance,
	// 		db:       db,
	// 	}

	// 	r = &DatabaseReconciler{
	// 		Client:   fakeClient,
	// 		Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
	// 		instance: &f,
	// 	}

	// 	// patching the Database object to reference the newly created Cluster
	// 	originalDatabase := publication.DeepCopy()
	// 	publication.Spec.ClusterRef.Name = "cluster-other"
	// 	Expect(fakeClient.Patch(ctx, publication, client.MergeFrom(originalDatabase))).To(Succeed())

	// 	// Reconcile and get the updated object
	// 	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
	// 		Namespace: publication.Namespace,
	// 		Name:      publication.Name,
	// 	}})
	// 	Expect(err).ToNot(HaveOccurred())

	// 	var updatedDatabase apiv1.Database
	// 	err = fakeClient.Get(ctx, client.ObjectKey{
	// 		Namespace: publication.Namespace,
	// 		Name:      publication.Name,
	// 	}, &updatedDatabase)
	// 	Expect(err).ToNot(HaveOccurred())

	// 	Expect(updatedDatabase.Status.Applied).Should(HaveValue(BeFalse()))
	// 	Expect(updatedDatabase.Status.Message).Should(ContainSubstring(`"cluster-other" not found`))
	// })

	// It("skips reconciliation if database object isn't found (deleted database)", func(ctx SpecContext) {
	// 	// Initialize a new Database but without creating it in the K8S Cluster
	// 	otherDatabase := &apiv1.Database{
	// 		ObjectMeta: metav1.ObjectMeta{
	// 			Name:       "db-other",
	// 			Namespace:  "default",
	// 			Generation: 1,
	// 		},
	// 		Spec: apiv1.DatabaseSpec{
	// 			ClusterRef: corev1.LocalObjectReference{
	// 				Name: cluster.Name,
	// 			},
	// 			Name:  "db-one",
	// 			Owner: "app",
	// 		},
	// 	}

	// 	// Reconcile the database that hasn't been created in the K8S Cluster
	// 	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
	// 		Namespace: otherDatabase.Namespace,
	// 		Name:      otherDatabase.Name,
	// 	}})

	// 	// Expect the reconciler to exit silently since the object doesn't exist
	// 	Expect(err).ToNot(HaveOccurred())
	// 	Expect(result).Should(BeZero()) // nothing to do, since the DB is being deleted
	// })

	// It("drops database with ensure absent option", func(ctx SpecContext) {
	// 	// Mocking dropDatabase
	// 	expectedValue := sqlmock.NewResult(0, 1)
	// 	expectedQuery := fmt.Sprintf(
	// 		"DROP DATABASE IF EXISTS %s",
	// 		pgx.Identifier{publication.Spec.Name}.Sanitize(),
	// 	)
	// 	dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedValue)

	// 	// Update the obj to set EnsureAbsent
	// 	publication.Spec.Ensure = apiv1.EnsureAbsent
	// 	Expect(fakeClient.Update(ctx, publication)).To(Succeed())

	// 	// Reconcile and get the updated object
	// 	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
	// 		Namespace: publication.Namespace,
	// 		Name:      publication.Name,
	// 	}})
	// 	Expect(err).ToNot(HaveOccurred())

	// 	err = fakeClient.Get(ctx, client.ObjectKey{
	// 		Namespace: publication.Namespace,
	// 		Name:      publication.Name,
	// 	}, publication)
	// 	Expect(err).ToNot(HaveOccurred())

	// 	Expect(publication.Status.Applied).To(HaveValue(BeTrue()))
	// 	Expect(publication.Status.Message).To(BeEmpty())
	// 	Expect(publication.Status.ObservedGeneration).To(BeEquivalentTo(1))
	// })

	// It("marks as failed if the target Database is already being managed", func(ctx SpecContext) {
	// 	// The Database obj currently managing "test-database"
	// 	currentManager := &apiv1.Database{
	// 		ObjectMeta: metav1.ObjectMeta{
	// 			Name:      "current-manager",
	// 			Namespace: "default",
	// 		},
	// 		Spec: apiv1.DatabaseSpec{
	// 			ClusterRef: corev1.LocalObjectReference{
	// 				Name: cluster.Name,
	// 			},
	// 			Name:  "test-database",
	// 			Owner: "app",
	// 		},
	// 		Status: apiv1.DatabaseStatus{
	// 			Applied:            ptr.To(true),
	// 			ObservedGeneration: 1,
	// 		},
	// 	}

	// 	// A new Database Object targeting the same "test-database"
	// 	dbDuplicate := &apiv1.Database{
	// 		ObjectMeta: metav1.ObjectMeta{
	// 			Name:       "db-duplicate",
	// 			Namespace:  "default",
	// 			Generation: 1,
	// 		},
	// 		Spec: apiv1.DatabaseSpec{
	// 			ClusterRef: corev1.LocalObjectReference{
	// 				Name: cluster.Name,
	// 			},
	// 			Name:  "test-database",
	// 			Owner: "app",
	// 		},
	// 	}

	// 	Expect(fakeClient.Create(ctx, currentManager)).To(Succeed())
	// 	Expect(fakeClient.Create(ctx, dbDuplicate)).To(Succeed())

	// 	// Reconcile and get the updated object
	// 	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
	// 		Namespace: dbDuplicate.Namespace,
	// 		Name:      dbDuplicate.Name,
	// 	}})
	// 	Expect(err).ToNot(HaveOccurred())

	// 	err = fakeClient.Get(ctx, client.ObjectKey{
	// 		Namespace: dbDuplicate.Namespace,
	// 		Name:      dbDuplicate.Name,
	// 	}, dbDuplicate)
	// 	Expect(err).ToNot(HaveOccurred())

	// 	expectedError := fmt.Sprintf("database %q is already managed by Database object %q",
	// 		dbDuplicate.Spec.Name, currentManager.Name)
	// 	Expect(dbDuplicate.Status.Applied).To(HaveValue(BeFalse()))
	// 	Expect(dbDuplicate.Status.Message).To(ContainSubstring(expectedError))
	// 	Expect(dbDuplicate.Status.ObservedGeneration).To(BeZero())
	// })

	// It("properly signals a database is on a replica cluster", func(ctx SpecContext) {
	// 	initialCluster := cluster.DeepCopy()
	// 	cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{
	// 		Enabled: ptr.To(true),
	// 	}
	// 	Expect(fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))).To(Succeed())

	// 	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
	// 		Namespace: publication.Namespace,
	// 		Name:      publication.Spec.Name,
	// 	}})
	// 	Expect(err).ToNot(HaveOccurred())

	// 	var updatedDatabase apiv1.Database
	// 	err = fakeClient.Get(ctx, client.ObjectKey{
	// 		Namespace: publication.Namespace,
	// 		Name:      publication.Name,
	// 	}, &updatedDatabase)
	// 	Expect(err).ToNot(HaveOccurred())

	// 	Expect(updatedDatabase.Status.Applied).Should(BeNil())
	// 	Expect(updatedDatabase.Status.Message).Should(ContainSubstring("waiting for the cluster to become primary"))
	// })
})
