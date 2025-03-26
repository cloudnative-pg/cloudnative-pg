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

package controller

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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

const publicationDetectionQuery = `SELECT count(*)
		FROM pg_catalog.pg_publication
		WHERE pubname = $1`

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
				Name:       "pub-one",
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

		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, publication).
			WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Publication{}).
			Build()

		r = &PublicationReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: pgInstance,
			getDB: func(_ string) (*sql.DB, error) {
				return db, nil
			},
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
		noHits := sqlmock.NewRows([]string{""}).AddRow("0")
		dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
			WillReturnRows(noHits)

		expectedCreate := sqlmock.NewResult(0, 1)
		expectedQuery := fmt.Sprintf(
			"CREATE PUBLICATION %s FOR ALL TABLES",
			pgx.Identifier{publication.Spec.Name}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

		err := reconcilePublication(ctx, fakeClient, r, publication)
		Expect(err).ToNot(HaveOccurred())

		Expect(publication.Status.Applied).Should(HaveValue(BeTrue()))
		Expect(publication.GetStatusMessage()).Should(BeEmpty())
		Expect(publication.GetFinalizers()).NotTo(BeEmpty())
	})

	It("publication object inherits error after patching", func(ctx SpecContext) {
		expectedError := fmt.Errorf("no permission")
		oneHit := sqlmock.NewRows([]string{""}).AddRow("1")
		dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
			WillReturnRows(oneHit)

		expectedQuery := fmt.Sprintf("ALTER PUBLICATION %s SET TABLES IN SCHEMA \"public\"",
			pgx.Identifier{publication.Spec.Name}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnError(expectedError)

		err := reconcilePublication(ctx, fakeClient, r, publication)
		Expect(err).ToNot(HaveOccurred())

		Expect(publication.Status.Applied).Should(HaveValue(BeFalse()))
		Expect(publication.Status.Message).Should(ContainSubstring(expectedError.Error()))
	})

	When("reclaim policy is delete", func() {
		It("on deletion it removes finalizers and drops the Publication", func(ctx SpecContext) {
			// Mocking Detect publication
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
				WillReturnRows(expectedValue)

			// Mocking Create publication
			expectedCreate := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE PUBLICATION %s FOR ALL TABLES",
				pgx.Identifier{publication.Spec.Name}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

			// Mocking Drop Publication
			expectedDrop := fmt.Sprintf("DROP PUBLICATION IF EXISTS %s",
				pgx.Identifier{publication.Spec.Name}.Sanitize(),
			)
			dbMock.ExpectExec(expectedDrop).WillReturnResult(sqlmock.NewResult(0, 1))

			err := reconcilePublication(ctx, fakeClient, r, publication)
			Expect(err).ToNot(HaveOccurred())

			// Plain successful reconciliation, finalizers have been created
			Expect(publication.GetFinalizers()).NotTo(BeEmpty())
			Expect(publication.Status.Applied).Should(HaveValue(BeTrue()))
			Expect(publication.Status.Message).Should(BeEmpty())

			// The next 2 lines are a hacky bit to make sure the next reconciler
			// call doesn't skip on account of Generation == ObservedGeneration.
			// See fake.Client known issues with `Generation`
			// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
			publication.SetGeneration(publication.GetGeneration() + 1)
			Expect(fakeClient.Update(ctx, publication)).To(Succeed())

			// We now look at the behavior when we delete the Database object
			Expect(fakeClient.Delete(ctx, publication)).To(Succeed())

			err = reconcilePublication(ctx, fakeClient, r, publication)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("reclaim policy is retain", func() {
		It("on deletion it removes finalizers and does NOT drop the Publication", func(ctx SpecContext) {
			publication.Spec.ReclaimPolicy = apiv1.PublicationReclaimRetain
			Expect(fakeClient.Update(ctx, publication)).To(Succeed())

			// Mocking Detect publication
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(publicationDetectionQuery).WithArgs(publication.Spec.Name).
				WillReturnRows(expectedValue)

			// Mocking Create publication
			expectedCreate := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE PUBLICATION %s FOR ALL TABLES",
				pgx.Identifier{publication.Spec.Name}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

			err := reconcilePublication(ctx, fakeClient, r, publication)
			Expect(err).ToNot(HaveOccurred())

			// Plain successful reconciliation, finalizers have been created
			Expect(publication.GetFinalizers()).NotTo(BeEmpty())
			Expect(publication.Status.Applied).Should(HaveValue(BeTrue()))
			Expect(publication.Status.Message).Should(BeEmpty())

			// The next 2 lines are a hacky bit to make sure the next reconciler
			// call doesn't skip on account of Generation == ObservedGeneration.
			// See fake.Client known issues with `Generation`
			// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
			publication.SetGeneration(publication.GetGeneration() + 1)
			Expect(fakeClient.Update(ctx, publication)).To(Succeed())

			// We now look at the behavior when we delete the Database object
			Expect(fakeClient.Delete(ctx, publication)).To(Succeed())

			err = reconcilePublication(ctx, fakeClient, r, publication)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	It("fails reconciliation if cluster isn't found (deleted cluster)", func(ctx SpecContext) {
		// Since the fakeClient has the `cluster-example` cluster, let's reference
		// another cluster `cluster-other` that is not found by the fakeClient
		pgInstance := postgres.NewInstance().
			WithNamespace("default").
			WithPodName("cluster-other-1").
			WithClusterName("cluster-other")

		r = &PublicationReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: pgInstance,
			getDB: func(_ string) (*sql.DB, error) {
				return db, nil
			},
		}

		// Updating the publication object to reference the newly created Cluster
		publication.Spec.ClusterRef.Name = "cluster-other"
		Expect(fakeClient.Update(ctx, publication)).To(Succeed())

		err := reconcilePublication(ctx, fakeClient, r, publication)
		Expect(err).ToNot(HaveOccurred())

		Expect(publication.Status.Applied).Should(HaveValue(BeFalse()))
		Expect(publication.GetStatusMessage()).Should(ContainSubstring(
			fmt.Sprintf("%q not found", publication.Spec.ClusterRef.Name)))
	})

	It("skips reconciliation if publication object isn't found (deleted publication)", func(ctx SpecContext) {
		// Initialize a new Publication but without creating it in the K8S Cluster
		otherPublication := &apiv1.Publication{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "pub-other",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.PublicationSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name: "pub-all",
			},
		}

		// Reconcile the publication that hasn't been created in the K8S Cluster
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: otherPublication.Namespace,
			Name:      otherPublication.Name,
		}})

		// Expect the reconciler to exit silently, since the object doesn't exist
		Expect(err).ToNot(HaveOccurred())
		Expect(result).Should(BeZero())
	})

	It("marks as failed if the target publication is already being managed", func(ctx SpecContext) {
		// Let's force the publication to have a past reconciliation
		publication.Status.ObservedGeneration = 2
		Expect(fakeClient.Status().Update(ctx, publication)).To(Succeed())

		// A new Publication Object targeting the same "pub-all"
		pubDuplicate := &apiv1.Publication{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "pub-duplicate",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.PublicationSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name: "pub-all",
			},
		}

		// Expect(fakeClient.Create(ctx, currentManager)).To(Succeed())
		Expect(fakeClient.Create(ctx, pubDuplicate)).To(Succeed())

		err := reconcilePublication(ctx, fakeClient, r, pubDuplicate)
		Expect(err).ToNot(HaveOccurred())

		expectedError := fmt.Sprintf("%q is already managed by object %q",
			pubDuplicate.Spec.Name, publication.Name)
		Expect(pubDuplicate.Status.Applied).To(HaveValue(BeFalse()))
		Expect(pubDuplicate.Status.Message).To(ContainSubstring(expectedError))
		Expect(pubDuplicate.Status.ObservedGeneration).To(BeZero())
	})

	It("properly signals a publication is on a replica cluster", func(ctx SpecContext) {
		initialCluster := cluster.DeepCopy()
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{
			Enabled: ptr.To(true),
		}
		Expect(fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))).To(Succeed())

		err := reconcilePublication(ctx, fakeClient, r, publication)
		Expect(err).ToNot(HaveOccurred())

		Expect(publication.Status.Applied).Should(BeNil())
		Expect(publication.Status.Message).Should(ContainSubstring("waiting for the cluster to become primary"))
	})
})

func reconcilePublication(
	ctx context.Context,
	fakeClient client.Client,
	r *PublicationReconciler,
	publication *apiv1.Publication,
) error {
	GinkgoT().Helper()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: publication.GetNamespace(),
		Name:      publication.GetName(),
	}})
	Expect(err).ToNot(HaveOccurred())
	return fakeClient.Get(ctx, client.ObjectKey{
		Namespace: publication.GetNamespace(),
		Name:      publication.GetName(),
	}, publication)
}
