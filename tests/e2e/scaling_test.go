package e2e

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	clusterv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster scale up and down", func() {

	It("can scale the cluster size", func() {
		const namespace = "cluster-scale-e2e-storage-class"
		const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
		const clusterName = "postgresql-storage-class"
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		}()
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Add a node to the cluster and verify the cluster has one more
		// element
		By("adding an instance to the cluster", func() {
			_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 300
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			Eventually(func() (int32, error) {
				cluster := &clusterv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(4))
		})

		// Remove a node from the cluster and verify the cluster has one
		// element less
		By("removing an instance from the cluster", func() {
			_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
			Expect(err).ToNot(HaveOccurred())
			timeout := 30
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			Eventually(func() (int32, error) {
				cluster := &clusterv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))
		})
	})
})
