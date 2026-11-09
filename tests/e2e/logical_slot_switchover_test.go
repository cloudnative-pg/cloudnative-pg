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
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/postgres/version"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A former primary keeps the failover slots it created, which are not marked
// as synced: PostgreSQL's slot sync worker refuses to overwrite them, so the
// operator must drop them for the worker to synchronize them again.
var _ = Describe("Logical Slot Switchover", Label(tests.LabelPublicationSubscription, tests.LabelSelfHealing), func() {
	const (
		clusterManifest = fixturesDir + "/logical_slot_switchover/cluster.yaml.template"
		namespacePrefix = "logical-slot-switchover"
		slotName        = "test_failover_slot"
		level           = tests.High
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		currentVersion, err := version.FromTag(env.PostgresImageTag)
		Expect(err).ToNot(HaveOccurred())
		if currentVersion.Major() < 17 {
			Skip("This test requires PostgreSQL 17 or later")
		}
	})

	It("synchronizes the failover slots of the former primary after a switchover", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)

		query := func(podName, sql string) func() (string, error) {
			return func() (string, error) {
				out, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{Namespace: namespace, PodName: podName},
					postgres.PostgresDBName, sql,
				)
				return strings.TrimSpace(out), err
			}
		}

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		oldPrimary := cluster.Status.CurrentPrimary
		var replica string
		for _, instance := range cluster.Status.InstanceNames {
			if instance != oldPrimary {
				replica = instance
			}
		}
		Expect(replica).ToNot(BeEmpty())

		By("creating a failover slot on the primary", func() {
			_, err := query(oldPrimary, "SELECT pg_catalog.pg_create_logical_replication_slot("+
				"'"+slotName+"', 'pgoutput', false, false, true)")()
			Expect(err).ToNot(HaveOccurred())
		})

		By("waiting for the replica to synchronize the slot", func() {
			// A synced slot stays temporary until it is safe to use, and a
			// promotion drops the temporary ones
			Eventually(query(replica, "SELECT synced AND NOT temporary FROM pg_catalog.pg_replication_slots "+
				"WHERE slot_name = '"+slotName+"'"), 120).Should(Equal("t"))
		})

		clusterasserts.AssertSwitchover(env, testTimeouts, namespace, clusterName)

		By("waiting for the former primary to synchronize the slot from the new primary", func() {
			Eventually(query(oldPrimary, "SELECT synced FROM pg_catalog.pg_replication_slots "+
				"WHERE slot_name = '"+slotName+"'"), 180).Should(Equal("t"))
		})
	})
})
