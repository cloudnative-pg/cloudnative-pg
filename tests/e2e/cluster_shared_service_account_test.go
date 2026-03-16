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
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Shared ServiceAccount", Label(tests.LabelBasic), func() {
	const (
		sharedSAFile    = fixturesDir + "/shared_service_account/cluster_shared_sa.yaml"
		cluster1File    = fixturesDir + "/shared_service_account/cluster_shared_sa_1.yaml"
		cluster1Name    = "cluster-shared-sa-1"
		namespacePrefix = "shared-sa-e2e"
		sharedSAName    = "cluster-shared-sa"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can use a shared ServiceAccount and preserves it after cluster deletion", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a shared ServiceAccount with operator pull secrets", func() {
			CreateResourceFromFile(namespace, sharedSAFile)
			operatorDeployment, err := operator.GetDeployment(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())
			Expect(secrets.CopyOperatorPullSecretToServiceAccount(
				env.Ctx, env.Client, operatorDeployment, namespace, sharedSAName,
			)).To(Succeed())
		})

		By("creating cluster using shared ServiceAccount", func() {
			AssertCreateCluster(namespace, cluster1Name, cluster1File, env)
		})

		By("verifying cluster pods use the shared ServiceAccount", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, cluster1Name)
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range podList.Items {
				Expect(pod.Spec.ServiceAccountName).To(Equal(sharedSAName),
					"Pod %s should use shared ServiceAccount %s", pod.Name, sharedSAName)
			}
		})

		By("deleting the cluster", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, cluster1Name)
			Expect(err).ToNot(HaveOccurred())

			Expect(objects.Delete(env.Ctx, env.Client, cluster)).To(Succeed())
		})

		By("verifying cluster is deleted", func() {
			Eventually(func() bool {
				_, err := clusterutils.Get(env.Ctx, env.Client, namespace, cluster1Name)
				return apierrs.IsNotFound(err)
			}, 120).Should(BeTrue())
		})

		By("verifying shared ServiceAccount still exists", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: sharedSAName},
				&sa)
			Expect(err).ToNot(HaveOccurred(),
				"Shared ServiceAccount should not be deleted when cluster is deleted")
		})
	})
})
