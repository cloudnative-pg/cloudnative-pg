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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// nolint: dupl
var _ = Describe("Test cleanup of owned objects on cluster deletion", func() {
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

	It("should set databases on the cluster as failed and delete their finalizers", func(ctx SpecContext) {
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
						ClusterRef: apiv1.ClusterObjectReference{
							Name: "cluster",
						},
					},
					Status: apiv1.DatabaseStatus{
						Applied: ptr.To(true),
						Message: "",
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
						ClusterRef: apiv1.ClusterObjectReference{
							Name: "cluster",
						},
					},
				},
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(databaseList).
			WithStatusSubresource(&databaseList.Items[0], &databaseList.Items[1]).Build()
		r.Client = cli
		err := r.notifyDeletionToOwnedResources(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		for _, db := range databaseList.Items {
			database := &apiv1.Database{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&db), database)
			Expect(err).ToNot(HaveOccurred())
			Expect(database.Finalizers).To(BeZero())
			Expect(database.Status.Applied).To(HaveValue(BeFalse()))
			Expect(database.Status.Message).To(ContainSubstring("cluster resource has been deleted"))
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
							ClusterRef: apiv1.ClusterObjectReference{
								Name: "another-cluster",
							},
						},
					},
				},
			}

			cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(databaseList).Build()
			r.Client = cli
			err := r.notifyDeletionToOwnedResources(ctx, namespacedName)
			Expect(err).ToNot(HaveOccurred())

			database := &apiv1.Database{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&databaseList.Items[0]), database)
			Expect(err).ToNot(HaveOccurred())
			Expect(database.Finalizers).To(BeEquivalentTo([]string{utils.DatabaseFinalizerName}))
			Expect(database.Status.Applied).To(BeNil())
			Expect(database.Status.Message).ToNot(ContainSubstring("not reconciled"))
		})

	It("should set publications on the cluster as failed and delete their finalizers", func(ctx SpecContext) {
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
					Status: apiv1.PublicationStatus{
						Applied: ptr.To(true),
						Message: "",
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

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(publicationList).
			WithStatusSubresource(&publicationList.Items[0], &publicationList.Items[1]).Build()
		r.Client = cli
		err := r.notifyDeletionToOwnedResources(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		for _, pub := range publicationList.Items {
			publication := &apiv1.Publication{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&pub), publication)
			Expect(err).ToNot(HaveOccurred())
			Expect(publication.Finalizers).To(BeZero())
			Expect(publication.Status.Applied).To(HaveValue(BeFalse()))
			Expect(publication.Status.Message).To(ContainSubstring("cluster resource has been deleted"))
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
		err := r.notifyDeletionToOwnedResources(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		publication := &apiv1.Publication{}
		err = cli.Get(ctx, client.ObjectKeyFromObject(&publicationList.Items[0]), publication)
		Expect(err).ToNot(HaveOccurred())
		Expect(publication.Finalizers).To(BeEquivalentTo([]string{utils.PublicationFinalizerName}))
		Expect(publication.Status.Applied).To(BeNil())
		Expect(publication.Status.Message).ToNot(ContainSubstring("not reconciled"))
	})

	It("should set subscriptions on the cluster as failed and delete their finalizers ", func(ctx SpecContext) {
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
					Status: apiv1.SubscriptionStatus{
						Applied: ptr.To(true),
						Message: "",
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

		cli := fake.NewClientBuilder().WithScheme(scheme).WithLists(subscriptionList).
			WithStatusSubresource(&subscriptionList.Items[0], &subscriptionList.Items[1]).Build()
		r.Client = cli
		err := r.notifyDeletionToOwnedResources(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		for _, sub := range subscriptionList.Items {
			subscription := &apiv1.Subscription{}
			err = cli.Get(ctx, client.ObjectKeyFromObject(&sub), subscription)
			Expect(err).ToNot(HaveOccurred())
			Expect(subscription.Finalizers).To(BeZero())
			Expect(subscription.Status.Applied).To(HaveValue(BeFalse()))
			Expect(subscription.Status.Message).To(ContainSubstring("cluster resource has been deleted"))
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
		err := r.notifyDeletionToOwnedResources(ctx, namespacedName)
		Expect(err).ToNot(HaveOccurred())

		subscription := &apiv1.Subscription{}
		err = cli.Get(ctx, client.ObjectKeyFromObject(&subscriptionList.Items[0]), subscription)
		Expect(err).ToNot(HaveOccurred())
		Expect(subscription.Finalizers).To(BeEquivalentTo([]string{utils.SubscriptionFinalizerName}))
		Expect(subscription.Status.Applied).To(BeNil())
		Expect(subscription.Status.Message).ToNot(ContainSubstring("not reconciled"))
	})
})

type testStruct struct{ Val int }

var _ = Describe("toSliceWithPointers", func() {
	It("should return pointers to the original slice elements", func() {
		items := []testStruct{{1}, {2}, {3}}
		pointers := toSliceWithPointers(items)
		Expect(pointers).To(HaveLen(len(items)))
		for i := range items {
			Expect(pointers[i]).To(BeIdenticalTo(&items[i]))
		}
	})
})
