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

package e2e

import (
	"k8s.io/apimachinery/pkg/api/meta"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster declarative hibernation", func() {
	const (
		sampleFileCluster = fixturesDir + "/base/cluster-storage-class.yaml.template"
		level             = tests.Medium
		tableName         = "test"
	)

	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("hibernates an existing cluster", func(ctx SpecContext) {
		const namespacePrefix = "declarative-hibernation"

		clusterName, err := env.GetResourceNameFromYAML(sampleFileCluster)
		Expect(err).ToNot(HaveOccurred())
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating a new cluster", func() {
			AssertCreateCluster(namespace, clusterName, sampleFileCluster, env)
			// Write a table and some data on the "app" database
			AssertCreateTestData(env, namespace, clusterName, tableName)
		})

		By("hibernating the new cluster", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			if cluster.Annotations == nil {
				cluster.Annotations = make(map[string]string)
			}
			originCluster := cluster.DeepCopy()
			cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOn

			Expect(env.Client.Patch(ctx, cluster, ctrlclient.MergeFrom(originCluster))).To(Succeed())
		})

		By("waiting for the cluster to be hibernated correctly", func() {
			Eventually(func(g Gomega) {
				cluster, err := env.GetCluster(namespace, clusterName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(meta.IsStatusConditionTrue(cluster.Status.Conditions, hibernation.HibernationConditionType)).To(BeTrue())
			}, 300).Should(Succeed())
		})

		By("verifying that the Pods have been deleted for the cluster", func() {
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			Expect(len(podList.Items)).Should(BeEquivalentTo(0))
		})

		By("rehydrating the cluster", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			if cluster.Annotations == nil {
				cluster.Annotations = make(map[string]string)
			}
			originCluster := cluster.DeepCopy()
			cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOff
			Expect(env.Client.Patch(ctx, cluster, ctrlclient.MergeFrom(originCluster))).To(Succeed())
		})

		var cluster *apiv1.Cluster
		By("waiting for the condition to be removed", func() {
			Eventually(func(g Gomega) {
				var err error
				cluster, err = env.GetCluster(namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())

				condition := meta.FindStatusCondition(cluster.Status.Conditions, hibernation.HibernationConditionType)
				g.Expect(condition).To(BeNil())
			}, 300).Should(Succeed())
		})

		By("waiting for the Pods to be recreated", func() {
			Eventually(func(g Gomega) {
				podList, _ := env.GetClusterPodList(namespace, clusterName)
				g.Expect(len(podList.Items)).Should(BeEquivalentTo(cluster.Spec.Instances))
			}, 300).Should(Succeed())
		})

		By("verifying the data has been preserved", func() {
			AssertDataExpectedCount(env, namespace, clusterName, tableName, 2)
		})
	})
})
