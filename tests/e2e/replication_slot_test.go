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

	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replication Slot", func() {
	const (
		namespace   = "replication-slot-e2e"
		sampleFile  = fixturesDir + "/replication_slot/cluster-pg-replication-slot.yaml.template"
		clusterName = "cluster-pg-replication-slot"
		level       = tests.High
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
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

	It("can manage Replication slots for HA", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Gather the current primary
		oldPrimaryPod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking Primary HA Slots exist and are active", func() {
			AssertRepSlotsOnPod(namespace, clusterName, *oldPrimaryPod)
		})

		By("Checking Standbys HA Slots exist", func() {
			replicaPods, err := env.GetClusterReplicas(namespace, clusterName)
			Expect(len(replicaPods.Items), err).To(BeEquivalentTo(2))
			for _, pod := range replicaPods.Items {
				AssertRepSlotsOnPod(namespace, clusterName, pod)
			}
		})

		By("Checking all the slots restart_lsn's are aligned", func() {
			AssertClusterRepSlotsAligned(namespace, clusterName)
		})

		By("Creating test data to advance streaming replication", func() {
			tableName := "data"
			AssertCreateTestData(namespace, clusterName, tableName)
			// Generate some WAL load
			query := fmt.Sprintf("INSERT INTO %v (SELECT generate_series(1,10000))",
				tableName)
			_, _, err = testsUtils.RunQueryFromPod(oldPrimaryPod, testsUtils.PGLocalSocketDir,
				"app", "postgres", "''", query, env)
			Expect(err).ToNot(HaveOccurred())
			_ = switchWalAndGetLatestArchive(namespace, oldPrimaryPod.Name)
		})

		By("Deleting the primary pod", func() {
			zero := int64(0)
			timeout := 120
			forceDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			err = env.DeletePod(namespace, oldPrimaryPod.Name, forceDelete)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, clusterName, timeout, env)
		})

		By("Checking that all the slots exist and are aligned", func() {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				AssertRepSlotsOnPod(namespace, clusterName, pod)
			}
			AssertClusterRepSlotsAligned(namespace, clusterName)
		})
	})
})
