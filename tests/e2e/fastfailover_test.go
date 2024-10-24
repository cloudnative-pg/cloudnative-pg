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

var _ = Describe("Fast failover", Serial, Label(tests.LabelPerformance, tests.LabelSelfHealing), func() {
	const (
		sampleFileWithoutReplicationSlots = fixturesDir + "/fastfailover/cluster-fast-failover.yaml.template"
		sampleFileWithReplicationSlots    = fixturesDir + "/fastfailover/cluster-fast-failover-with-repl-slots.yaml.template"
		sampleFileSyncReplicas            = fixturesDir + "/fastfailover/cluster-syncreplicas-fast-failover.yaml.template"
		webTestFile                       = fixturesDir + "/fastfailover/webtest.yaml"
		webTestSyncReplicas               = fixturesDir + "/fastfailover/webtest-syncreplicas.yaml"
		webTestJob                        = fixturesDir + "/fastfailover/apache-benchmark-webtest.yaml"
		level                             = tests.Highest
	)
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

		// The walreceiver of a standby that wasn't promoted may try to reconnect
		// before the rw service endpoints are updated. In this case, the walreceiver
		// can be stuck for waiting for the connection to be established for a time that
		// depends on the tcp_syn_retries sysctl. Since by default
		// net.ipv4.tcp_syn_retries=6, PostgreSQL can wait 2^7-1=127 seconds before
		// restarting the walreceiver.
		if !IsLocal() {
			maxReattachTime = 180
			maxFailoverTime = 30
		}
	})

	Context("with async replicas cluster (without HA Replication Slots)", func() {
		// Confirm that a standby closely following the primary doesn't need more
		// than 10 seconds to be promoted and be able to start inserting records.
		// We test this setting up an application pointing to the rw service,
		// forcing a failover and measuring how much time passes between the
		// last row written on timeline 1 and the first one on timeline 2.
		It("can do a fast failover", func() {
			const namespacePrefix = "primary-failover-time-async"
			clusterName = "cluster-fast-failover"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertFastFailOver(namespace, sampleFileWithoutReplicationSlots, clusterName,
				webTestFile, webTestJob, maxReattachTime, maxFailoverTime)
		})
	})

	Context("with async replicas cluster and HA Replication Slots", func() {
		// Confirm that a standby closely following the primary doesn't need more
		// than 10 seconds to be promoted and be able to start inserting records.
		// We test this setting up an application pointing to the rw service,
		// forcing a failover and measuring how much time passes between the
		// last row written on timeline 1 and the first one on timeline 2.
		It("can do a fast failover", func() {
			const namespacePrefix = "primary-failover-time-async-with-slots"
			clusterName = "cluster-fast-failover"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertFastFailOver(namespace, sampleFileWithReplicationSlots,
				clusterName, webTestFile, webTestJob, maxReattachTime, maxFailoverTime)
			AssertClusterHAReplicationSlots(namespace, clusterName)
		})
	})

	Context("with sync replicas cluster", func() {
		It("can do a fast failover", func() {
			const namespacePrefix = "primary-failover-time-sync-replicas"
			clusterName = "cluster-syncreplicas-fast-failover"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertFastFailOver(
				namespace, sampleFileSyncReplicas, clusterName, webTestSyncReplicas, webTestJob, maxReattachTime, maxFailoverTime)
		})
	})
})
