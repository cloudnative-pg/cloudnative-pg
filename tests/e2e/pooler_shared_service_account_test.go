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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler Shared ServiceAccount", Label(tests.LabelBasic), func() {
	const (
		clusterFile     = fixturesDir + "/shared_service_account/cluster_basic.yaml"
		sharedSAFile    = fixturesDir + "/shared_service_account/pooler_shared_sa.yaml"
		pooler1File     = fixturesDir + "/shared_service_account/pooler_shared_sa_1.yaml"
		clusterName     = "cluster-pooler-shared-sa"
		pooler1Name     = "pooler-shared-sa-1"
		namespacePrefix = "pooler-shared-sa-e2e"
		sharedSAName    = "pooler-shared-sa"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can use a shared ServiceAccount and preserves it after pooler deletion", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a cluster for pooler", func() {
			AssertCreateCluster(namespace, clusterName, clusterFile, env)
		})

		By("creating a shared ServiceAccount with operator pull secrets", func() {
			CreateResourceFromFile(namespace, sharedSAFile)
			operatorDeployment, err := operator.GetDeployment(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())
			Expect(secrets.CopyOperatorPullSecretToServiceAccount(
				env.Ctx, env.Client, operatorDeployment, namespace, sharedSAName,
			)).To(Succeed())
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

		By("verifying pooler deployment uses the shared ServiceAccount", func() {
			var deployment appsv1.Deployment
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: pooler1Name},
				&deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(sharedSAName))
		})

		By("verifying pooler pods use the shared ServiceAccount", func() {
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

		By("verifying shared ServiceAccount still exists", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: sharedSAName},
				&sa)
			Expect(err).ToNot(HaveOccurred(),
				"Shared ServiceAccount should not be deleted when pooler is deleted")
		})
	})
})
