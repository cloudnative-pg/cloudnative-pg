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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pooler_controller unit tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	It("should make sure that getPoolersUsingSecret works correctly", func() {
		var poolers []apiv1.Pooler
		var expectedContent []types.NamespacedName
		var nonExpectedContent []types.NamespacedName
		var expectedAuthSecretName string

		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		By("creating expected poolers", func() {
			pooler1 := *newFakePooler(env.client, cluster)
			expectedAuthSecretName = pooler1.GetAuthQuerySecretName()

			pooler2 := *newFakePooler(env.client, cluster)
			pooler3 := *newFakePooler(env.client, cluster)
			for _, expectedPooler := range []apiv1.Pooler{pooler1, pooler2, pooler3} {
				poolers = append(poolers, expectedPooler)
				expectedContent = append(
					expectedContent,
					types.NamespacedName{Name: expectedPooler.Name, Namespace: expectedPooler.Namespace},
				)
			}
		})

		By("creating pooler that should be skipped", func() {
			cluster := newFakeCNPGCluster(env.client, namespace)
			pooler := *newFakePooler(env.client, cluster)
			nn := types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}
			nonExpectedContent = append(nonExpectedContent, nn)
			poolers = append(poolers, pooler)
		})

		By("making sure only expected poolers are fetched", func() {
			poolerList := apiv1.PoolerList{Items: poolers}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      expectedAuthSecretName,
					Namespace: namespace,
				},
			}
			reqs := getPoolersUsingSecret(poolerList, secret)

			Expect(reqs).To(HaveLen(len(expectedContent)))
			Expect(reqs).To(Equal(expectedContent))
		})
	})

	It("should make sure to create a request for any pooler owned secret", func() {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		pooler1 := *newFakePooler(env.client, cluster)
		pooler2 := *newFakePooler(env.client, cluster)
		poolerList := apiv1.PoolerList{Items: []apiv1.Pooler{pooler1, pooler2}}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "random-image-pull-secret",
				Namespace: namespace,
			},
		}

		err := ctrl.SetControllerReference(&pooler1, secret, schemeBuilder.BuildWithAllKnownScheme())
		Expect(err).ToNot(HaveOccurred())

		req := getPoolersUsingSecret(poolerList, secret)
		Expect(req).To(HaveLen(1))
		Expect(req[0]).To(Equal(types.NamespacedName{
			Name:      pooler1.Name,
			Namespace: pooler1.Namespace,
		}))
	})

	It("should make sure that mapSecretToPooler produces the correct requests", func() {
		var expectedRequests []reconcile.Request
		var nonExpectedRequests []reconcile.Request
		var expectedAuthSecretName string

		ctx := context.Background()
		namespace1 := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace1)

		By("creating expected poolers", func() {
			pooler1 := *newFakePooler(env.client, cluster)
			pooler2 := *newFakePooler(env.client, cluster)
			expectedAuthSecretName = pooler1.GetAuthQuerySecretName()

			for _, expectedPooler := range []apiv1.Pooler{pooler1, pooler2} {
				request := reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      expectedPooler.Name,
						Namespace: expectedPooler.Namespace,
					},
				}
				expectedRequests = append(expectedRequests, request)
			}
		})

		By("creating pooler with a different secret that should be skipped", func() {
			pooler3 := *newFakePooler(env.client, cluster)
			pooler3.Spec.PgBouncer.AuthQuerySecret = &apiv1.LocalObjectReference{
				Name: "test-one",
			}
			pooler3.Spec.PgBouncer.AuthQuery = "SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1"
			err := env.client.Update(ctx, &pooler3)
			Expect(err).ToNot(HaveOccurred())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pooler3.Name,
					Namespace: pooler3.Namespace,
				},
			}

			nonExpectedRequests = append(nonExpectedRequests, req)
		})

		By("creating a pooler in a different namespace that should be skipped", func() {
			namespace2 := newFakeNamespace(env.client)
			cluster2 := newFakeCNPGCluster(env.client, namespace2)
			pooler := *newFakePooler(env.client, cluster2)
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				},
			}
			nonExpectedRequests = append(nonExpectedRequests, req)
		})

		By("making sure the function builds up the correct reconcile requests", func() {
			handler := env.poolerReconciler.mapSecretToPooler()
			reReqs := handler(
				ctx,
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      expectedAuthSecretName,
						Namespace: namespace1,
					},
				},
			)

			for _, expectedRequest := range expectedRequests {
				Expect(reReqs).To(ContainElement(expectedRequest))
			}
			for _, nonExpectedRequest := range nonExpectedRequests {
				Expect(reReqs).ToNot(ContainElement(nonExpectedRequest))
			}
		})
	})

	It("should make sure that isOwnedByPoolerKind works correctly", func() {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := *newFakePooler(env.client, cluster)

		By("making sure it returns true when the resource is owned by a pooler", func() {
			ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
			utils.SetAsOwnedBy(&ownedResource.ObjectMeta, pooler.ObjectMeta, pooler.TypeMeta)

			name, owned := isOwnedByPoolerKind(&ownedResource)
			Expect(owned).To(BeTrue())
			Expect(name).To(Equal(pooler.Name))
		})

		By("making sure it returns false when the resource is not owned by a pooler", func() {
			ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
			utils.SetAsOwnedBy(&ownedResource.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

			name, owned := isOwnedByPoolerKind(&ownedResource)
			Expect(owned).To(BeFalse())
			Expect(name).To(Equal(""))
		})
	})
})

var _ = Describe("isOwnedByPooler function tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	It("should return true if the object is owned by the specified pooler", func() {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := *newFakePooler(env.client, cluster)

		ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
		utils.SetAsOwnedBy(&ownedResource.ObjectMeta, pooler.ObjectMeta, pooler.TypeMeta)

		result := isOwnedByPooler(pooler.Name, &ownedResource)
		Expect(result).To(BeTrue())
	})

	It("should return false if the object is not owned by the specified pooler", func() {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := *newFakePooler(env.client, cluster)

		anotherPooler := *newFakePooler(env.client, cluster)
		ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
		utils.SetAsOwnedBy(&ownedResource.ObjectMeta, anotherPooler.ObjectMeta, anotherPooler.TypeMeta)

		result := isOwnedByPooler(pooler.Name, &ownedResource)
		Expect(result).To(BeFalse())
	})

	It("should return false if the object is not owned by any pooler", func() {
		namespace := newFakeNamespace(env.client)
		ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}

		result := isOwnedByPooler("some-pooler", &ownedResource)
		Expect(result).To(BeFalse())
	})

	It("should return false if the object is owned by a different kind", func() {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := *newFakePooler(env.client, cluster)

		ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
		utils.SetAsOwnedBy(&ownedResource.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

		result := isOwnedByPooler(pooler.Name, &ownedResource)
		Expect(result).To(BeFalse())
	})
})
