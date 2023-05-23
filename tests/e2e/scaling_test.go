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
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster scale up and down", Serial, Label(tests.LabelReplication), func() {
	const (
		sampleFileWithoutReplicationSlots = fixturesDir + "/base/cluster-storage-class.yaml.template"
		sampleFileWithReplicationSlots    = fixturesDir + "/base/cluster-storage-class-with-rep-slots.yaml.template"
		clusterName                       = "postgresql-storage-class"
		level                             = tests.Lowest
		expectedPvcCount                  = 6
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("with HA Replication Slots", func() {
		It("can scale the cluster size", func() {
			const namespacePrefix = "cluster-scale-e2e-with-slots"
			// Create a cluster in a namespace we'll delete after the test
			namespace, err := env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespaceAndWait(namespace, 60)
			})
			AssertCreateCluster(namespace, clusterName, sampleFileWithReplicationSlots, env)

			AssertClusterReplicationSlots(clusterName, namespace)
			// Add a node to the cluster and verify the cluster has one more
			// element
			By("adding an instance to the cluster", func() {
				_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
				Expect(err).ToNot(HaveOccurred())
				timeout := 300
				AssertClusterIsReady(namespace, clusterName, timeout, env)
			})
			AssertPvcHasLabels(namespace, clusterName)
			AssertClusterReplicationSlots(clusterName, namespace)

			// Remove a node from the cluster and verify the cluster has one
			// element less
			By("removing an instance from the cluster", func() {
				_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
				Expect(err).ToNot(HaveOccurred())
				timeout := 60
				AssertClusterIsReady(namespace, clusterName, timeout, env)
			})
			AssertClusterReplicationSlots(clusterName, namespace)

			By("verify pvc pgWal and pgData are deleted after scale down", func() {
				AssertPVCCount(namespace, clusterName, expectedPvcCount, 60)
			})
		})
	})

	Context("without HA Replication Slots", func() {
		It("can scale the cluster size", func() {
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "cluster-scale-e2e"
			namespace, err := env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespaceAndWait(namespace, 60)
			})
			AssertCreateCluster(namespace, clusterName, sampleFileWithoutReplicationSlots, env)

			// Add a node to the cluster and verify the cluster has one more
			// element
			By("adding an instance to the cluster", func() {
				_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
				Expect(err).ToNot(HaveOccurred())
				timeout := 300
				AssertClusterIsReady(namespace, clusterName, timeout, env)
			})
			AssertPvcHasLabels(namespace, clusterName)

			// Remove a node from the cluster and verify the cluster has one
			// element less
			By("removing an instance from the cluster", func() {
				_, _, err := utils.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
				Expect(err).ToNot(HaveOccurred())
				timeout := 60
				AssertClusterIsReady(namespace, clusterName, timeout, env)
			})
			By("verify pvc pgWal and pgData are deleted after scale down", func() {
				AssertPVCCount(namespace, clusterName, expectedPvcCount, 60)
			})
		})
	})
})
