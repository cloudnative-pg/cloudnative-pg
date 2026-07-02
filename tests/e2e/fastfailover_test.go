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
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	replicationasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"

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
		maxReattachTime = 60
		maxFailoverTime int
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// The primary is deleted abruptly (quickDeletionPeriod): usually it still
		// releases the primary lease cleanly inside the grace period and a replica
		// promotes within a couple of seconds, but if the SIGKILL wins the race the
		// lease is left held, and the promoting replica must observe it unchanged
		// for a full lease duration and then claim it on its next poll before it can
		// promote. That take-over window is engine-independent and dominates the
		// failover when it happens, so use it (plus a small margin for the promotion
		// itself) as a floor on the budget rather than adding it to the per-engine
		// base.
		abruptTakeOver := apiv1.DefaultPrimaryLeaseDurationSeconds + apiv1.DefaultPrimaryLeaseRetryPeriodSeconds + 3
		maxFailoverTime = 10
		if !(IsKind() || IsK3D()) {
			maxFailoverTime = 30
		}
		if maxFailoverTime < abruptTakeOver {
			maxFailoverTime = abruptTakeOver
		}
	})

	Context("with async replicas cluster (without HA Replication Slots)", func() {
		// Confirm that a standby closely following the primary is promoted and
		// resumes inserting records within the failover budget. We test this
		// setting up an application pointing to the rw service, forcing a
		// failover and measuring how much time passes between the last row
		// written on timeline 1 and the first one on timeline 2.
		It("can do a fast failover", func() {
			const namespacePrefix = "primary-failover-time-async"
			clusterName = "cluster-fast-failover"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			replicationasserts.AssertFastFailOver(
				env, testTimeouts,
				namespace, sampleFileWithoutReplicationSlots, clusterName,
				webTestFile, webTestJob,
				maxReattachTime, maxFailoverTime, quickDeletionPeriod,
			)
		})
	})

	Context("with async replicas cluster and HA Replication Slots", func() {
		// Confirm that a standby closely following the primary is promoted and
		// resumes inserting records within the failover budget. We test this
		// setting up an application pointing to the rw service, forcing a
		// failover and measuring how much time passes between the last row
		// written on timeline 1 and the first one on timeline 2.
		It("can do a fast failover", func() {
			const namespacePrefix = "primary-failover-time-async-with-slots"
			clusterName = "cluster-fast-failover"
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			replicationasserts.AssertFastFailOver(
				env, testTimeouts,
				namespace, sampleFileWithReplicationSlots, clusterName,
				webTestFile, webTestJob,
				maxReattachTime, maxFailoverTime, quickDeletionPeriod,
			)

			replicationasserts.AssertClusterHAReplicationSlots(env, namespace, clusterName)
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
			replicationasserts.AssertFastFailOver(
				env, testTimeouts,
				namespace, sampleFileSyncReplicas, clusterName,
				webTestSyncReplicas, webTestJob,
				maxReattachTime, maxFailoverTime, quickDeletionPeriod,
			)
		})
	})
})
