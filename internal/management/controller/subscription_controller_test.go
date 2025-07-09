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
	"github.com/lib/pq"
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

const subscriptionDetectionQuery = `SELECT count(*)
		FROM pg_catalog.pg_subscription
		WHERE subname = $1`

var _ = Describe("Managed subscription controller tests", func() {
	const defaultPostgresMajorVersion = 17

	var (
		dbMock       sqlmock.Sqlmock
		db           *sql.DB
		subscription *apiv1.Subscription
		cluster      *apiv1.Cluster
		r            *SubscriptionReconciler
		fakeClient   client.Client
		connString   string
		err          error
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
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "cluster-other",
						ConnectionParameters: map[string]string{
							"host": "localhost",
						},
					},
				},
			},
		}
		subscription = &apiv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sub-one",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.SubscriptionSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				ReclaimPolicy:       apiv1.SubscriptionReclaimDelete,
				Name:                "sub-one",
				DBName:              "app",
				PublicationName:     "pub-all",
				PublicationDBName:   "app",
				ExternalClusterName: "cluster-other",
			},
		}
		connString, err = getSubscriptionConnectionString(cluster, "cluster-other", "app")
		Expect(err).ToNot(HaveOccurred())

		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		pgInstance := postgres.NewInstance().
			WithNamespace("default").
			WithPodName("cluster-example-1").
			WithClusterName("cluster-example")

		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, subscription).
			WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Subscription{}).
			Build()

		r = &SubscriptionReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: pgInstance,
			getDB: func(_ string) (*sql.DB, error) {
				return db, nil
			},
			getPostgresMajorVersion: func() (int, error) {
				return defaultPostgresMajorVersion, nil
			},
		}
		r.finalizerReconciler = newFinalizerReconciler(
			fakeClient,
			utils.SubscriptionFinalizerName,
			r.evaluateDropSubscription,
		)
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("adds finalizer and sets status ready on success", func(ctx SpecContext) {
		noHits := sqlmock.NewRows([]string{""}).AddRow("0")
		dbMock.ExpectQuery(subscriptionDetectionQuery).WithArgs(subscription.Spec.Name).
			WillReturnRows(noHits)

		expectedCreate := sqlmock.NewResult(0, 1)
		expectedQuery := fmt.Sprintf(
			"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
			pgx.Identifier{subscription.Spec.Name}.Sanitize(),
			pq.QuoteLiteral(connString),
			pgx.Identifier{subscription.Spec.PublicationName}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: subscription.GetNamespace(),
			Name:      subscription.GetName(),
		}})
		Expect(err).ToNot(HaveOccurred())
		err = fakeClient.Get(ctx, client.ObjectKey{
			Namespace: subscription.GetNamespace(),
			Name:      subscription.GetName(),
		}, subscription)
		Expect(err).ToNot(HaveOccurred())

		Expect(subscription.Status.Applied).Should(HaveValue(BeTrue()))
		Expect(subscription.GetStatusMessage()).Should(BeEmpty())
		Expect(subscription.GetFinalizers()).NotTo(BeEmpty())
	})

	It("subscription object inherits error after patching", func(ctx SpecContext) {
		expectedError := fmt.Errorf("no permission")
		oneHit := sqlmock.NewRows([]string{""}).AddRow("1")
		dbMock.ExpectQuery(subscriptionDetectionQuery).WithArgs(subscription.Spec.Name).
			WillReturnRows(oneHit)

		expectedQuery := fmt.Sprintf("ALTER SUBSCRIPTION %s SET PUBLICATION %s",
			pgx.Identifier{subscription.Spec.Name}.Sanitize(),
			pgx.Identifier{subscription.Spec.PublicationName}.Sanitize(),
		)
		dbMock.ExpectExec(expectedQuery).WillReturnError(expectedError)

		err = reconcileSubscription(ctx, fakeClient, r, subscription)
		Expect(err).ToNot(HaveOccurred())

		Expect(subscription.Status.Applied).Should(HaveValue(BeFalse()))
		Expect(subscription.Status.Message).Should(ContainSubstring(expectedError.Error()))
	})

	When("reclaim policy is delete", func() {
		It("on deletion it removes finalizers and drops the subscription", func(ctx SpecContext) {
			// Mocking detection of subscriptions
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(subscriptionDetectionQuery).WithArgs(subscription.Spec.Name).
				WillReturnRows(expectedValue)

			// Mocking create subscription
			expectedCreate := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
				pgx.Identifier{subscription.Spec.Name}.Sanitize(),
				pq.QuoteLiteral(connString),
				pgx.Identifier{subscription.Spec.PublicationName}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

			// Mocking Drop subscription
			expectedDrop := fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s",
				pgx.Identifier{subscription.Spec.Name}.Sanitize(),
			)
			dbMock.ExpectExec(expectedDrop).WillReturnResult(sqlmock.NewResult(0, 1))

			err = reconcileSubscription(ctx, fakeClient, r, subscription)
			Expect(err).ToNot(HaveOccurred())

			// Plain successful reconciliation, finalizers have been created
			Expect(subscription.GetFinalizers()).NotTo(BeEmpty())
			Expect(subscription.Status.Applied).Should(HaveValue(BeTrue()))
			Expect(subscription.Status.Message).Should(BeEmpty())

			// The next 2 lines are a hacky bit to make sure the next reconciler
			// call doesn't skip on account of Generation == ObservedGeneration.
			// See fake.Client known issues with `Generation`
			// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
			subscription.SetGeneration(subscription.GetGeneration() + 1)
			Expect(fakeClient.Update(ctx, subscription)).To(Succeed())

			// We now look at the behavior when we delete the Database object
			Expect(fakeClient.Delete(ctx, subscription)).To(Succeed())

			err = reconcileSubscription(ctx, fakeClient, r, subscription)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("reclaim policy is retain", func() {
		It("on deletion it removes finalizers and does NOT drop the subscription", func(ctx SpecContext) {
			subscription.Spec.ReclaimPolicy = apiv1.SubscriptionReclaimRetain
			Expect(fakeClient.Update(ctx, subscription)).To(Succeed())

			// Mocking Detect subscription
			expectedValue := sqlmock.NewRows([]string{""}).AddRow("0")
			dbMock.ExpectQuery(subscriptionDetectionQuery).WithArgs(subscription.Spec.Name).
				WillReturnRows(expectedValue)

			// Mocking Create subscription
			expectedCreate := sqlmock.NewResult(0, 1)
			expectedQuery := fmt.Sprintf(
				"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
				pgx.Identifier{subscription.Spec.Name}.Sanitize(),
				pq.QuoteLiteral(connString),
				pgx.Identifier{subscription.Spec.PublicationName}.Sanitize(),
			)
			dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)

			err = reconcileSubscription(ctx, fakeClient, r, subscription)
			Expect(err).ToNot(HaveOccurred())

			// Plain successful reconciliation, finalizers have been created
			Expect(subscription.GetFinalizers()).NotTo(BeEmpty())
			Expect(subscription.Status.Applied).Should(HaveValue(BeTrue()))
			Expect(subscription.Status.Message).Should(BeEmpty())

			// The next 2 lines are a hacky bit to make sure the next reconciler
			// call doesn't skip on account of Generation == ObservedGeneration.
			// See fake.Client known issues with `Generation`
			// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
			subscription.SetGeneration(subscription.GetGeneration() + 1)
			Expect(fakeClient.Update(ctx, subscription)).To(Succeed())

			// We now look at the behavior when we delete the Database object
			Expect(fakeClient.Delete(ctx, subscription)).To(Succeed())

			err = reconcileSubscription(ctx, fakeClient, r, subscription)
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

		r = &SubscriptionReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: pgInstance,
			getDB: func(_ string) (*sql.DB, error) {
				return db, nil
			},
		}

		// Updating the subscription object to reference the newly created Cluster
		subscription.Spec.ClusterRef.Name = "cluster-other"
		Expect(fakeClient.Update(ctx, subscription)).To(Succeed())

		err = reconcileSubscription(ctx, fakeClient, r, subscription)
		Expect(err).ToNot(HaveOccurred())

		Expect(subscription.Status.Applied).Should(HaveValue(BeFalse()))
		Expect(subscription.Status.Message).Should(ContainSubstring(
			fmt.Sprintf("%q not found", subscription.Spec.ClusterRef.Name)))
	})

	It("skips reconciliation if subscription object isn't found (deleted subscription)", func(ctx SpecContext) {
		// Initialize a new subscription but without creating it in the K8S Cluster
		otherSubscription := &apiv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sub-other",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.SubscriptionSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name: "sub-one",
			},
		}

		// Reconcile the subscription that hasn't been created in the K8S Cluster
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: otherSubscription.Namespace,
			Name:      otherSubscription.Name,
		}})

		// Expect the reconciler to exit silently, since the object doesn't exist
		Expect(err).ToNot(HaveOccurred())
		Expect(result).Should(BeZero()) // nothing to do, since the subscription is being deleted
	})

	It("marks as failed if the target subscription is already being managed", func(ctx SpecContext) {
		// Let's force the subscription to have a past reconciliation
		subscription.Status.ObservedGeneration = 2
		Expect(fakeClient.Status().Update(ctx, subscription)).To(Succeed())

		// A new subscription Object targeting the same "sub-one"
		subDuplicate := &apiv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sub-duplicate",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: apiv1.SubscriptionSpec{
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
				Name:                "sub-one",
				PublicationName:     "pub-all",
				PublicationDBName:   "app",
				ExternalClusterName: "cluster-other",
			},
		}

		// Expect(fakeClient.Create(ctx, currentManager)).To(Succeed())
		Expect(fakeClient.Create(ctx, subDuplicate)).To(Succeed())

		err = reconcileSubscription(ctx, fakeClient, r, subDuplicate)
		Expect(err).ToNot(HaveOccurred())

		expectedError := fmt.Sprintf("%q is already managed by object %q",
			subDuplicate.Spec.Name, subscription.Name)
		Expect(subDuplicate.Status.Applied).Should(HaveValue(BeFalse()))
		Expect(subDuplicate.Status.Message).Should(ContainSubstring(expectedError))
	})

	It("properly signals a subscription is on a replica cluster", func(ctx SpecContext) {
		initialCluster := cluster.DeepCopy()
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{
			Enabled: ptr.To(true),
		}
		Expect(fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))).To(Succeed())

		err = reconcileSubscription(ctx, fakeClient, r, subscription)
		Expect(err).ToNot(HaveOccurred())

		Expect(subscription.Status.Applied).Should(BeNil())
		Expect(subscription.Status.Message).Should(ContainSubstring("waiting for the cluster to become primary"))
	})
})

func reconcileSubscription(
	ctx context.Context,
	fakeClient client.Client,
	r *SubscriptionReconciler,
	subscription *apiv1.Subscription,
) error {
	GinkgoT().Helper()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: subscription.GetNamespace(),
		Name:      subscription.GetName(),
	}})
	Expect(err).ToNot(HaveOccurred())
	return fakeClient.Get(ctx, client.ObjectKey{
		Namespace: subscription.GetNamespace(),
		Name:      subscription.GetName(),
	}, subscription)
}
