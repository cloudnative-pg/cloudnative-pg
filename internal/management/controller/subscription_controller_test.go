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
	"github.com/lib/pq"
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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	subscriptionDetectionQuery = `SELECT count(*)
		FROM pg_subscription
		WHERE subname = $1`
)

var _ = Describe("Managed subscription controller tests", func() {
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
				ExternalClusters: apiv1.ExternalClusterList{
					apiv1.ExternalCluster{
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

		f := fakeInstanceData{
			Instance: pgInstance,
			db:       db,
		}

		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, subscription).
			WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Subscription{}).
			Build()

		r = &SubscriptionReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: &f,
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
		assertObjectWasReconciled(ctx, r, subscription, &apiv1.Subscription{}, fakeClient,
			func() {
				// Mocking Detect
				noHits := sqlmock.NewRows([]string{""}).AddRow("0")
				dbMock.ExpectQuery(subscriptionDetectionQuery).WithArgs(subscription.Spec.Name).
					WillReturnRows(noHits)

				// Mocking Create subscription
				expectedCreate := sqlmock.NewResult(0, 1)
				expectedQuery := fmt.Sprintf(
					"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
					pgx.Identifier{subscription.Spec.Name}.Sanitize(),
					pq.QuoteLiteral(connString),
					pgx.Identifier{subscription.Spec.PublicationName}.Sanitize(),
				)
				dbMock.ExpectExec(expectedQuery).WillReturnResult(expectedCreate)
			},
			func(updatedSubscription *apiv1.Subscription) {
				Expect(updatedSubscription.GetStatusMessage()).Should(BeEmpty())
				Expect(updatedSubscription.GetStatusApplied()).Should(HaveValue(BeTrue()))
				Expect(updatedSubscription.GetFinalizers()).NotTo(BeEmpty())
			},
		)
	})

	It("subscription object inherits error after patching", func(ctx SpecContext) {
		expectedError := fmt.Errorf("no permission")
		assertObjectWasReconciled(ctx, r, subscription, &apiv1.Subscription{}, fakeClient,
			func() {
				// Mocking Detect
				oneHit := sqlmock.NewRows([]string{""}).AddRow("1")
				dbMock.ExpectQuery(subscriptionDetectionQuery).WithArgs(subscription.Spec.Name).
					WillReturnRows(oneHit)

				// Mocking Alter subscription

				expectedQuery := fmt.Sprintf("ALTER SUBSCRIPTION %s SET PUBLICATION %s",
					pgx.Identifier{subscription.Spec.Name}.Sanitize(),
					pgx.Identifier{subscription.Spec.PublicationName}.Sanitize(),
				)
				dbMock.ExpectExec(expectedQuery).WillReturnError(expectedError)
			},
			func(updatedSubscription *apiv1.Subscription) {
				Expect(updatedSubscription.Status.Applied).Should(HaveValue(BeFalse()))
				Expect(updatedSubscription.Status.Message).Should(ContainSubstring(expectedError.Error()))
			},
		)
	})

	When("retention policy is delete", func() {
		It("on deletion it removes finalizers and drops the subscription", func(ctx SpecContext) {
			assertObjectReconciledAfterDeletion(ctx, r, subscription, &apiv1.Subscription{}, fakeClient,
				func() {
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
				},
			)
		})
	})

	When("retention policy is retain", func() {
		It("on deletion it removes finalizers and does NOT drop the subscription", func(ctx SpecContext) {
			subscription.Spec.ReclaimPolicy = apiv1.SubscriptionReclaimRetain
			Expect(fakeClient.Update(ctx, subscription)).To(Succeed())

			assertObjectReconciledAfterDeletion(ctx, r, subscription, &apiv1.Subscription{}, fakeClient,
				func() {
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
				},
			)
		})
	})

	It("fails reconciliation if cluster isn't found (deleted cluster)", func(ctx SpecContext) {
		// since the fakeClient has the `cluster-example` cluster, let's reference
		// another cluster `cluster-other` that is not found by the fakeClient
		pgInstance := postgres.NewInstance().
			WithNamespace("default").
			WithPodName("cluster-other-1").
			WithClusterName("cluster-other")

		f := fakeInstanceData{
			Instance: pgInstance,
			db:       db,
		}

		r = &SubscriptionReconciler{
			Client:   fakeClient,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
			instance: &f,
		}

		// updating the subscription object to reference the newly created Cluster
		subscription.Spec.ClusterRef.Name = "cluster-other"
		Expect(fakeClient.Update(ctx, subscription)).To(Succeed())

		assertObjectWasReconciled(ctx, r, subscription, &apiv1.Subscription{}, fakeClient,
			func() {
				// no interactions expected with Postgres
			},
			func(updatedSubscription *apiv1.Subscription) {
				Expect(updatedSubscription.Status.Applied).Should(HaveValue(BeFalse()))
				Expect(updatedSubscription.Status.Message).Should(ContainSubstring(`"cluster-other" not found`))
			},
		)
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

		// Expect the reconciler to exit silently since the object doesn't exist
		Expect(err).ToNot(HaveOccurred())
		Expect(result).Should(BeZero()) // nothing to do, since the subscription is being deleted
	})

	It("marks as failed if the target subscription is already being managed", func(ctx SpecContext) {
		// Let's force the subscription to have a past reconciliation
		subscription.SetObservedGeneration(2)
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

		assertObjectWasReconciled(ctx, r, subDuplicate, &apiv1.Subscription{}, fakeClient,
			func() {
				// No interactions expected with Postgres
			},
			func(updatedSubscription *apiv1.Subscription) {
				expectedError := fmt.Sprintf("%q is already managed by object %q",
					subDuplicate.Spec.Name, subscription.Name)
				Expect(updatedSubscription.Status.Applied).To(HaveValue(BeFalse()))
				Expect(updatedSubscription.Status.Message).To(ContainSubstring(expectedError))
				Expect(updatedSubscription.Status.ObservedGeneration).To(BeZero())
			},
		)
	})

	It("properly signals a subscription is on a replica cluster", func(ctx SpecContext) {
		initialCluster := cluster.DeepCopy()
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{
			Enabled: ptr.To(true),
		}
		Expect(fakeClient.Patch(ctx, cluster, client.MergeFrom(initialCluster))).To(Succeed())

		assertObjectWasReconciled(ctx, r, subscription, &apiv1.Subscription{}, fakeClient,
			func() {
				// No interactions expected with Postgres
			},
			func(updatedSubscription *apiv1.Subscription) {
				Expect(updatedSubscription.Status.Applied).Should(BeNil())
				Expect(updatedSubscription.Status.Message).Should(ContainSubstring("waiting for the cluster to become primary"))
			},
		)
	})
})
