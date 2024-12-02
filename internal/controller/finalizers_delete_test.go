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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// nolint: dupl
var _ = Describe("CRD finalizers", func() {
	var (
		r              ClusterReconciler
		scheme         *runtime.Scheme
		namespacedName types.NamespacedName
	)

	BeforeEach(func() {
		scheme = schemeBuilder.BuildWithAllKnownScheme()
		r = ClusterReconciler{
			Scheme: scheme,
		}
		namespacedName = types.NamespacedName{
			Namespace: "test",
			Name:      "cluster",
		}
	})

	It("should delete database finalizers for databases on the cluster", func(ctx SpecContext) {
		databaseList := &apiv1.DatabaseList{
			Items: []apiv1.Database{
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.DatabaseFinalizerName,
						},
						Name:      "db-1",
						Namespace: "test",
					},
					Spec: apiv1.DatabaseSpec{
						Name: "db-test",
						ClusterRef: corev1.LocalObjectReference{
							Name: "cluster",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.DatabaseFinalizerName,
						},
						Name:      "db-2",
						Namespace: "test",
					},
					Spec: apiv1.DatabaseSpec{
						Name: "db-test-2",
						ClusterRef: corev1.LocalObjectReference{
							Name: "cluster",
						},
					},
				},
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(databaseList).Build()
		r.Client = cli
		err := r.deleteFinalizers(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		for _, db := range databaseList.Items {
			database := &apiv1.Database{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&db), database)
			Expect(err).ToNot(HaveOccurred())
			Expect(database.Finalizers).To(BeZero())
		}
	})

	It("should not delete database finalizers for databases in another cluster",
		func(ctx SpecContext) {
			databaseList := &apiv1.DatabaseList{
				Items: []apiv1.Database{
					{
						ObjectMeta: metav1.ObjectMeta{
							Finalizers: []string{
								utils.DatabaseFinalizerName,
							},
							Name:      "db-1",
							Namespace: "test",
						},
						Spec: apiv1.DatabaseSpec{
							Name: "db-test",
							ClusterRef: corev1.LocalObjectReference{
								Name: "another-cluster",
							},
						},
					},
				},
			}

			cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(databaseList).Build()
			r.Client = cli
			err := r.deleteFinalizers(ctx, namespacedName)
			Expect(err).ToNot(HaveOccurred())

			database := &apiv1.Database{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&databaseList.Items[0]), database)
			Expect(err).ToNot(HaveOccurred())
			Expect(database.Finalizers).To(BeEquivalentTo([]string{utils.DatabaseFinalizerName}))
		})

	It("should delete publication finalizers for publications on the cluster", func(ctx SpecContext) {
		publicationList := &apiv1.PublicationList{
			Items: []apiv1.Publication{
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.PublicationFinalizerName,
						},
						Name:      "pub-1",
						Namespace: "test",
					},
					Spec: apiv1.PublicationSpec{
						Name: "pub-test",
						ClusterRef: corev1.LocalObjectReference{
							Name: "cluster",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.PublicationFinalizerName,
						},
						Name:      "pub-2",
						Namespace: "test",
					},
					Spec: apiv1.PublicationSpec{
						Name: "pub-test-2",
						ClusterRef: corev1.LocalObjectReference{
							Name: "cluster",
						},
					},
				},
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(publicationList).Build()
		r.Client = cli
		err := r.deleteFinalizers(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		for _, pub := range publicationList.Items {
			publication := &apiv1.Publication{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&pub), publication)
			Expect(err).ToNot(HaveOccurred())
			Expect(publication.Finalizers).To(BeZero())
		}
	})

	It("should not delete publication finalizers for publications in another cluster", func(ctx SpecContext) {
		publicationList := &apiv1.PublicationList{
			Items: []apiv1.Publication{
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.PublicationFinalizerName,
						},
						Name:      "pub-1",
						Namespace: "test",
					},
					Spec: apiv1.PublicationSpec{
						Name: "pub-test",
						ClusterRef: corev1.LocalObjectReference{
							Name: "another-cluster",
						},
					},
				},
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(publicationList).Build()
		r.Client = cli
		err := r.deleteFinalizers(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		publication := &apiv1.Publication{}
		err = cli.Get(ctx, client.ObjectKeyFromObject(&publicationList.Items[0]), publication)
		Expect(err).ToNot(HaveOccurred())
		Expect(publication.Finalizers).To(BeEquivalentTo([]string{utils.PublicationFinalizerName}))
	})

	It("should delete subscription finalizers for subscriptions on the cluster", func(ctx SpecContext) {
		subscriptionList := &apiv1.SubscriptionList{
			Items: []apiv1.Subscription{
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.SubscriptionFinalizerName,
						},
						Name:      "sub-1",
						Namespace: "test",
					},
					Spec: apiv1.SubscriptionSpec{
						Name: "sub-test",
						ClusterRef: corev1.LocalObjectReference{
							Name: "cluster",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.SubscriptionFinalizerName,
						},
						Name:      "sub-2",
						Namespace: "test",
					},
					Spec: apiv1.SubscriptionSpec{
						Name: "sub-test-2",
						ClusterRef: corev1.LocalObjectReference{
							Name: "cluster",
						},
					},
				},
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(subscriptionList).Build()
		r.Client = cli
		err := r.deleteFinalizers(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		for _, sub := range subscriptionList.Items {
			subscription := &apiv1.Subscription{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&sub), subscription)
			Expect(err).ToNot(HaveOccurred())
			Expect(subscription.Finalizers).To(BeZero())
		}
	})

	It("should not delete subscription finalizers for subscriptions in another cluster", func(ctx SpecContext) {
		subscriptionList := &apiv1.SubscriptionList{
			Items: []apiv1.Subscription{
				{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{
							utils.SubscriptionFinalizerName,
						},
						Name:      "sub-1",
						Namespace: "test",
					},
					Spec: apiv1.SubscriptionSpec{
						Name: "sub-test",
						ClusterRef: corev1.LocalObjectReference{
							Name: "another-cluster",
						},
					},
				},
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(subscriptionList).Build()
		r.Client = cli
		err := r.deleteFinalizers(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		subscription := &apiv1.Subscription{}
		err = cli.Get(ctx, client.ObjectKeyFromObject(&subscriptionList.Items[0]), subscription)
		Expect(err).ToNot(HaveOccurred())
		Expect(subscription.Finalizers).To(BeEquivalentTo([]string{utils.SubscriptionFinalizerName}))
	})
})
