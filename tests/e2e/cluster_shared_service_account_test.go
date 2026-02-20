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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Shared ServiceAccount", Label(tests.LabelBasic), func() {
	const (
		sharedSAFile    = fixturesDir + "/shared_service_account/cluster_shared_sa.yaml"
		cluster1File    = fixturesDir + "/shared_service_account/cluster_shared_sa_1.yaml"
		cluster2File    = fixturesDir + "/shared_service_account/cluster_shared_sa_2.yaml"
		cluster1Name    = "cluster-shared-sa-1"
		cluster2Name    = "cluster-shared-sa-2"
		namespacePrefix = "shared-sa-e2e"
		sharedSAName    = "cluster-shared-sa"
		level           = tests.Medium
	)

	var namespace string
	var err error

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can use a shared ServiceAccount across multiple clusters", func() {
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a shared ServiceAccount", func() {
			CreateResourceFromFile(namespace, sharedSAFile)
		})

		By("creating first cluster using shared ServiceAccount", func() {
			AssertCreateCluster(namespace, cluster1Name, cluster1File, env)
		})

		By("verifying first cluster pods use the shared ServiceAccount", func() {
			podList, err := pods.List(env.Ctx, env.Client, namespace)
			Expect(err).ToNot(HaveOccurred())

			// Filter pods belonging to cluster1
			cluster1Pods := []corev1.Pod{}
			for _, pod := range podList.Items {
				if pod.Labels["cnpg.io/cluster"] == cluster1Name {
					cluster1Pods = append(cluster1Pods, pod)
				}
			}
			Expect(cluster1Pods).ToNot(BeEmpty())

			for _, pod := range cluster1Pods {
				Expect(pod.Spec.ServiceAccountName).To(Equal(sharedSAName),
					"Pod %s should use shared ServiceAccount %s", pod.Name, sharedSAName)
			}
		})

		By("verifying no ServiceAccount was created with cluster name", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: cluster1Name},
				&sa)
			Expect(err).To(HaveOccurred())
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		By("creating second cluster using the same shared ServiceAccount", func() {
			AssertCreateCluster(namespace, cluster2Name, cluster2File, env)
		})

		By("verifying second cluster pods also use the shared ServiceAccount", func() {
			podList, err := pods.List(env.Ctx, env.Client, namespace)
			Expect(err).ToNot(HaveOccurred())

			// Filter pods belonging to cluster2
			cluster2Pods := []corev1.Pod{}
			for _, pod := range podList.Items {
				if pod.Labels["cnpg.io/cluster"] == cluster2Name {
					cluster2Pods = append(cluster2Pods, pod)
				}
			}
			Expect(cluster2Pods).ToNot(BeEmpty())

			for _, pod := range cluster2Pods {
				Expect(pod.Spec.ServiceAccountName).To(Equal(sharedSAName),
					"Pod %s should use shared ServiceAccount %s", pod.Name, sharedSAName)
			}
		})

		By("verifying the shared ServiceAccount still exists", func() {
			var sa corev1.ServiceAccount
			err := env.Client.Get(env.Ctx,
				ctrlclient.ObjectKey{Namespace: namespace, Name: sharedSAName},
				&sa)
			Expect(err).ToNot(HaveOccurred())
			Expect(sa.Name).To(Equal(sharedSAName))
		})
	})

	It("should fail when specified ServiceAccount does not exist", func() {
		const (
			clusterFailFile = fixturesDir + "/shared_service_account/cluster_nonexists_sa.yaml"
			clusterFailName = "cluster-nonexistent-sa"
		)

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating cluster with non-existent ServiceAccount reference", func() {
			CreateResourceFromFile(namespace, clusterFailFile)
		})

		By("verifying cluster reports error about missing ServiceAccount", func() {
			Eventually(func() bool {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterFailName)
				if err != nil {
					return false
				}

				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, 60).Should(BeTrue())
		})
	})
})
