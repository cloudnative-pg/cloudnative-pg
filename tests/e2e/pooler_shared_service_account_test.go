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

package e2e

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler Shared ServiceAccount", Label(tests.LabelBasic), func() {
	const (
		clusterFile           = fixturesDir + "/shared_service_account/cluster_basic.yaml"
		sharedSAFile          = fixturesDir + "/shared_service_account/pooler_shared_sa.yaml"
		pooler1File           = fixturesDir + "/shared_service_account/pooler_shared_sa_1.yaml"
		pooler2File           = fixturesDir + "/shared_service_account/pooler_shared_sa_2.yaml"
		poolerNonExistsSAFile = fixturesDir + "/shared_service_account/pooler_nonexists_sa.yaml"
		clusterName           = "cluster-pooler-shared-sa"
		pooler1Name           = "pooler-shared-sa-1"
		pooler2Name           = "pooler-shared-sa-2"
		poolerNonExistsSAName = "pooler-nonexistent-sa"
		namespacePrefix       = "pooler-shared-sa-e2e"
		sharedSAName          = "pooler-shared-sa"
		level                 = tests.Medium
	)

	var namespace string
	var err error

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can use a shared ServiceAccount across multiple poolers", func() {
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster for poolers", func() {
			AssertCreateCluster(namespace, clusterName, clusterFile, env)
		})

		By("creating a shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, sharedSAFile)
		})

		By("verifying shared ServiceAccount was created", func() {
			Eventually(func() error {
				var sa corev1.ServiceAccount
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: sharedSAName},
					&sa)
			}, 30).Should(Succeed())
		})

		By("creating first pooler using shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, pooler1File)
		})

		By("waiting for first pooler deployment to be ready", func() {
			Eventually(func() error {
				var deployment appsv1.Deployment
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler1Name},
					&deployment)
			}, 300).Should(Succeed())
		})

		By("verifying first pooler deployment uses the shared ServiceAccount", func() {
			var deployment appsv1.Deployment
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: pooler1Name},
				&deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(sharedSAName))
		})

		By("verifying first pooler pods use the shared ServiceAccount", func() {
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{utils.PgbouncerNameLabel: pooler1Name})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).ToNot(BeEmpty())

				for _, pod := range podList.Items {
					g.Expect(pod.Spec.ServiceAccountName).To(Equal(sharedSAName),
						"Pod %s should use shared ServiceAccount %s", pod.Name, sharedSAName)
				}
			}, 60).Should(Succeed())
		})

		By("verifying no ServiceAccount was created with pooler name", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: pooler1Name},
				&sa)
			Expect(err).To(HaveOccurred())
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		By("verifying RoleBinding references the shared ServiceAccount", func() {
			var roleBinding rbacv1.RoleBinding
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: pooler1Name},
				&roleBinding)
			Expect(err).ToNot(HaveOccurred())

			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(sharedSAName),
				"RoleBinding should reference shared ServiceAccount")
			Expect(roleBinding.Subjects[0].Namespace).To(Equal(namespace))
		})

		By("creating second pooler using the same shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, pooler2File)
		})

		By("waiting for second pooler deployment to be ready", func() {
			Eventually(func() error {
				var deployment appsv1.Deployment
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler2Name},
					&deployment)
			}, 300).Should(Succeed())
		})

		By("verifying second pooler pods also use the shared ServiceAccount", func() {
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{utils.PgbouncerNameLabel: pooler2Name})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).ToNot(BeEmpty())

				for _, pod := range podList.Items {
					g.Expect(pod.Spec.ServiceAccountName).To(Equal(sharedSAName),
						"Pod %s should use shared ServiceAccount %s", pod.Name, sharedSAName)
				}
			}, 60).Should(Succeed())
		})

		By("verifying the shared ServiceAccount still exists", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: sharedSAName},
				&sa)
			Expect(err).ToNot(HaveOccurred())
			Expect(sa.Name).To(Equal(sharedSAName))
		})
	})

	It("should fail when specified ServiceAccount does not exist", func() {
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster for pooler", func() {
			AssertCreateCluster(namespace, clusterName, clusterFile, env)
		})

		By("creating pooler with non-existent ServiceAccount reference", func() {
			CreateResourceFromFile(namespace, poolerNonExistsSAFile)
		})

		By("verifying pooler resource was created", func() {
			Eventually(func() error {
				pooler := &apiv1.Pooler{}
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: poolerNonExistsSAName},
					pooler)
			}, 60).Should(Succeed())
		})

		By("verifying the non-existent ServiceAccount is not created", func() {
			Consistently(func() bool {
				sa := &corev1.ServiceAccount{}
				err := env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: poolerNonExistsSAName}, sa)
				return apierrs.IsNotFound(err)
			}, 30, 5).Should(BeTrue(), "ServiceAccount should not be created by operator")
		})

		By("verifying deployment is NOT created due to validation failure", func() {
			Consistently(func() bool {
				deployment := &appsv1.Deployment{}
				err := env.Client.Get(env.Ctx,
					types.NamespacedName{
						Namespace: namespace,
						Name:      poolerNonExistsSAName,
					},
					deployment)
				return apierrs.IsNotFound(err)
			}, 60, 5).Should(BeTrue(), "Deployment should not be created when ServiceAccount doesn't exist")
		})

		By("verifying pooler remains non-operational", func() {
			Consistently(func() bool {
				pooler := &apiv1.Pooler{}
				err := env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: poolerNonExistsSAName},
					pooler)
				if err != nil {
					return false
				}

				deployment := &appsv1.Deployment{}
				err = env.Client.Get(env.Ctx,
					types.NamespacedName{
						Namespace: namespace,
						Name:      poolerNonExistsSAName,
					},
					deployment)

				return apierrs.IsNotFound(err)
			}, 30, 5).Should(BeTrue(), "Pooler should remain non-operational without valid ServiceAccount")
		})
	})

	It("should cleanup RoleBinding but not shared ServiceAccount when pooler is deleted", func() {
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster for pooler", func() {
			AssertCreateCluster(namespace, clusterName, clusterFile, env)
		})

		By("creating a shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, sharedSAFile)
		})

		By("verifying shared ServiceAccount was created", func() {
			Eventually(func() error {
				var sa corev1.ServiceAccount
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: sharedSAName},
					&sa)
			}, 30).Should(Succeed())
		})

		By("creating pooler using shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, pooler1File)
		})

		By("waiting for pooler deployment to be ready", func() {
			Eventually(func() error {
				var deployment appsv1.Deployment
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler1Name},
					&deployment)
			}, 300).Should(Succeed())
		})

		By("verifying RoleBinding was created", func() {
			Eventually(func() error {
				var rb rbacv1.RoleBinding
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler1Name},
					&rb)
			}, 30).Should(Succeed())
		})

		By("deleting the pooler", func() {
			pooler := &apiv1.Pooler{}
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: pooler1Name},
				pooler)
			Expect(err).ToNot(HaveOccurred())

			err = env.Client.Delete(env.Ctx, pooler)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying pooler is deleted", func() {
			Eventually(func() bool {
				pooler := &apiv1.Pooler{}
				err := env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler1Name},
					pooler)
				return apierrs.IsNotFound(err)
			}, 60).Should(BeTrue())
		})

		By("verifying RoleBinding is cleaned up", func() {
			Eventually(func() bool {
				var rb rbacv1.RoleBinding
				err := env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler1Name},
					&rb)
				return apierrs.IsNotFound(err)
			}, 60).Should(BeTrue())
		})

		By("verifying shared ServiceAccount still exists (not deleted)", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: sharedSAName},
				&sa)
			Expect(err).ToNot(HaveOccurred(),
				"Shared ServiceAccount should not be deleted when pooler is deleted")
		})
	})

	It("can mix poolers with shared and managed ServiceAccounts", func() {
		const poolerManagedName = "pooler-managed-sa"

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster for poolers", func() {
			AssertCreateCluster(namespace, clusterName, clusterFile, env)
		})

		By("creating a shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, sharedSAFile)
		})

		By("verifying shared ServiceAccount was created", func() {
			Eventually(func() error {
				var sa corev1.ServiceAccount
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: sharedSAName},
					&sa)
			}, 30).Should(Succeed())
		})

		By("creating pooler with shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, pooler1File)
		})

		By("creating pooler without serviceAccountName (operator-managed)", func() {
			instance := new(int32)
			*instance = 1
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      poolerManagedName,
					Namespace: namespace,
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{
						Name: clusterName,
					},
					Instances: instance,
					Type:      apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						PoolMode: apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.Client.Create(env.Ctx, pooler)
			Expect(err).ToNot(HaveOccurred())
		})

		By("verifying pooler with shared SA uses shared ServiceAccount", func() {
			Eventually(func() (string, error) {
				var deployment appsv1.Deployment
				err := env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: pooler1Name},
					&deployment)
				if err != nil {
					return "", err
				}
				return deployment.Spec.Template.Spec.ServiceAccountName, nil
			}, 120).Should(Equal(sharedSAName))
		})

		By("verifying pooler without serviceAccountName creates its own ServiceAccount", func() {
			Eventually(func() error {
				var sa corev1.ServiceAccount
				return env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: poolerManagedName},
					&sa)
			}, 120).Should(Succeed())

			var deployment appsv1.Deployment
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: poolerManagedName},
				&deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(poolerManagedName))
		})
	})
})
