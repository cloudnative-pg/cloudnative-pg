package e2e

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
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

		var cluster apiv1.Cluster
		clusterName, err := env.GetResourceNameFromYAML(sampleFileCluster)
		Expect(err).ToNot(HaveOccurred())
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}

		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		By("creating a new cluster", func() {
			AssertCreateCluster(namespace, clusterName, sampleFileCluster, env)
			// Write a table and some data on the "app" database
			AssertCreateTestData(namespace, clusterName, tableName, psqlClientPod)
		})

		By("hibernating the new cluster", func() {
			Expect(env.Client.Get(ctx, namespacedName, &cluster)).To(Succeed())
			if cluster.Annotations == nil {
				cluster.Annotations = make(map[string]string)
			}
			originCluster := cluster.DeepCopy()
			cluster.Annotations[hibernation.HibernationAnnotationName] = hibernation.HibernationOn

			Expect(env.Client.Patch(ctx, &cluster, ctrlclient.MergeFrom(originCluster))).To(Succeed())
		})

		By("waiting for the cluster to be hibernated correctly", func() {
			Eventually(func(g Gomega) {
				g.Expect(env.Client.Get(ctx, namespacedName, &cluster)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(cluster.Status.Conditions, hibernation.HibernationConditionType)).To(BeTrue())
			}, 300).Should(Succeed())
		})

		By("verifying that the Pods have been deleted for the cluster", func() {
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			Expect(len(podList.Items)).Should(BeEquivalentTo(0))
		})

		By("rehydrating the cluster", func() {
			Expect(env.Client.Get(ctx, namespacedName, &cluster)).To(Succeed())
			if cluster.Annotations == nil {
				cluster.Annotations = make(map[string]string)
			}
			originCluster := cluster.DeepCopy()
			cluster.Annotations[hibernation.HibernationAnnotationName] = hibernation.HibernationOff
			Expect(env.Client.Patch(ctx, &cluster, ctrlclient.MergeFrom(originCluster))).To(Succeed())
		})

		By("waiting for the condition to be removed", func() {
			Eventually(func(g Gomega) {
				g.Expect(env.Client.Get(ctx, namespacedName, &cluster)).To(Succeed())

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
			AssertDataExpectedCount(namespace, clusterName, tableName, 2, psqlClientPod)
		})
	})
})
