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

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pooler_controller unit tests", func() {
	It("should make sure that getPoolersUsingSecret works correctly", func() {
		var poolers []v1.Pooler
		var expectedContent []types.NamespacedName
		var nonExpectedContent []types.NamespacedName
		var expectedAuthSecretName string

		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)

		By("creating expected poolers", func() {
			pooler1 := *newFakePooler(cluster)
			expectedAuthSecretName = pooler1.GetAuthQuerySecretName()

			pooler2 := *newFakePooler(cluster)
			pooler3 := *newFakePooler(cluster)
			for _, expectedPooler := range []v1.Pooler{pooler1, pooler2, pooler3} {
				poolers = append(poolers, expectedPooler)
				expectedContent = append(
					expectedContent,
					types.NamespacedName{Name: expectedPooler.Name, Namespace: expectedPooler.Namespace},
				)
			}
		})

		By("creating pooler that should be skipped", func() {
			cluster := newFakeCNPGCluster(namespace)
			pooler := *newFakePooler(cluster)
			nn := types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}
			nonExpectedContent = append(nonExpectedContent, nn)
			poolers = append(poolers, pooler)
		})

		By("making sure only expected poolers are fetched", func() {
			poolerList := v1.PoolerList{Items: poolers}
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

	It("should make sure that mapSecretToPooler produces the correct requests", func() {
		var expectedRequests []reconcile.Request
		var nonExpectedRequests []reconcile.Request
		var expectedAuthSecretName string

		ctx := context.Background()
		namespace1 := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace1)

		By("creating expected poolers", func() {
			pooler1 := *newFakePooler(cluster)
			pooler2 := *newFakePooler(cluster)
			expectedAuthSecretName = pooler1.GetAuthQuerySecretName()

			for _, expectedPooler := range []v1.Pooler{pooler1, pooler2} {
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
			pooler3 := *newFakePooler(cluster)
			pooler3.Spec.PgBouncer.AuthQuerySecret = &v1.LocalObjectReference{
				Name: "test-one",
			}
			pooler3.Spec.PgBouncer.AuthQuery = "SELECT usename, passwd FROM pg_shadow WHERE usename=$1"
			err := k8sClient.Update(ctx, &pooler3)
			Expect(err).To(BeNil())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pooler3.Name,
					Namespace: pooler3.Namespace,
				},
			}

			nonExpectedRequests = append(nonExpectedRequests, req)
		})

		By("creating a pooler in a different namespace that should be skipped", func() {
			namespace2 := newFakeNamespace()
			cluster2 := newFakeCNPGCluster(namespace2)
			pooler := *newFakePooler(cluster2)
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pooler.Name,
					Namespace: pooler.Namespace,
				},
			}
			nonExpectedRequests = append(nonExpectedRequests, req)
		})

		By("making sure the function builds up the correct reconcile requests", func() {
			handler := poolerReconciler.mapSecretToPooler()
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

	It("should make sure that isOwnedByPooler works correctly", func() {
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		pooler := *newFakePooler(cluster)

		By("making sure it returns true when the resource is owned by a pooler", func() {
			ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
			utils.SetAsOwnedBy(&ownedResource.ObjectMeta, pooler.ObjectMeta, pooler.TypeMeta)

			name, owned := isOwnedByPooler(&ownedResource)
			Expect(owned).To(BeTrue())
			Expect(name).To(Equal(pooler.Name))
		})

		By("making sure it returns false when the resource is not owned by a pooler", func() {
			ownedResource := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "example-service", Namespace: namespace}}
			utils.SetAsOwnedBy(&ownedResource.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

			name, owned := isOwnedByPooler(&ownedResource)
			Expect(owned).To(BeFalse())
			Expect(name).To(Equal(""))
		})
	})
})
