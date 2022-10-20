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
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Switchover", Serial, func() {
	const (
		namespace                         = "switchover-e2e"
		sampleFileWithoutReplicationSlots = fixturesDir + "/base/cluster-storage-class.yaml.template"
		sampleFileWithReplicationSlots    = fixturesDir + "/base/cluster-storage-class-with-rep-slots.yaml.template"
		clusterName                       = "postgresql-storage-class"
		level                             = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	Context("with HA Replication slots", func() {
		It("reacts to switchover requests", func() {
			if env.PostgresVersion == 10 {
				Skip("replication slots not available for PostgreSQL 10 or older")
			}
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespaceAndWait(namespace, 60)
			})

			AssertCreateCluster(namespace, clusterName, sampleFileWithReplicationSlots, env)
			AssertSwitchover(namespace, clusterName, env)
			AssertPvcHasLabels(namespace, clusterName)
			AssertClusterReplicationSlots(namespace, clusterName)
		})
	})
	Context("without HA Replication slots", func() {
		It("reacts to switchover requests", func() {
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespaceAndWait(namespace, 60)
			})

			AssertCreateCluster(namespace, clusterName, sampleFileWithoutReplicationSlots, env)
			AssertSwitchover(namespace, clusterName, env)
			AssertPvcHasLabels(namespace, clusterName)
		})
	})
})
