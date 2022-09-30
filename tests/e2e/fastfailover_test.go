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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fast failover", Serial, Label(tests.LabelPerformance), func() {
	const (
		sampleFileWithoutRepSlots = fixturesDir + "/fastfailover/cluster-fast-failover.yaml.template"
		sampleFileWithRepSlots    = fixturesDir + "/fastfailover/cluster-fast-failover-with-repl-slots.yaml.template"
		sampleFileSyncReplicas    = fixturesDir + "/fastfailover/cluster-syncreplicas-fast-failover.yaml.template"
		webTestFile               = fixturesDir + "/fastfailover/webtest.yaml"
		webTestSyncReplicas       = fixturesDir + "/fastfailover/webtest-syncreplicas.yaml"
		webTestJob                = fixturesDir + "/fastfailover/apache-benchmark-webtest.yaml"
		level                     = tests.Highest
	)
	sampleFile := sampleFileWithRepSlots
	var (
		namespace       string
		clusterName     string
		maxReattachTime int32 = 60
		maxFailoverTime int32 = 10
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if env.IsIBM() {
			Skip("This test is not run on an IBM architecture")
		}
		// Sometimes on AKS the promotion itself takes more than 10 seconds.
		// Nothing to be done operator side, we raise the timeout to avoid
		// failures in the test.
		isAKS, err := env.IsAKS()
		if err != nil {
			fmt.Println("Couldn't verify if tests are running on AKS, assuming they aren't")
		}

		if isAKS {
			maxFailoverTime = 30
		}

		// GKE has a higher kube-proxy timeout, and the connections could try
		// using a service, for which the routing table hasn't changed, getting
		// stuck for a while.
		// We raise the timeout, since we can't intervene on GKE configuration.
		isGKE, err := env.IsGKE()
		if err != nil {
			fmt.Println("Couldn't verify if tests are running on GKE, assuming they aren't")
		}

		if isGKE {
			maxReattachTime = 180
			maxFailoverTime = 20
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("with async replicas cluster", func() {
		// Confirm that a standby closely following the primary doesn't need more
		// than 10 seconds to be promoted and be able to start inserting records.
		// We test this setting up an application pointing to the rw service,
		// forcing a failover and measuring how much time passes between the
		// last row written on timeline 1 and the first one on timeline 2.
		It("can do a fast failover", func() {
			if env.PostgresVersion == 10 {
				// Cluster file without replication slot since it requires PostgreSQL 11 or above
				sampleFile = sampleFileWithoutRepSlots
			}
			namespace = "primary-failover-time"
			clusterName = "cluster-fast-failover"
			AssertFastFailOver(namespace, sampleFile, clusterName, webTestFile, webTestJob, maxReattachTime, maxFailoverTime)
			AssertClusterReplicationSlots(namespace, clusterName)
		})
	})

	Context("with sync replicas cluster", func() {
		It("can do a fast failover", func() {
			namespace = "primary-failover-time-sync-replicas"
			clusterName = "cluster-syncreplicas-fast-failover"
			AssertFastFailOver(
				namespace, sampleFileSyncReplicas, clusterName, webTestSyncReplicas, webTestJob, maxReattachTime, maxFailoverTime)
		})
	})
})
