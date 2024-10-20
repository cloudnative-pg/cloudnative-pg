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
	"strconv"
	"strings"

	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Synchronous Replicas", Label(tests.LabelReplication), func() {
	const level = tests.Medium
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	getSyncReplicationCount := func(namespace, clusterName, syncState string, expectedCount int) {
		Eventually(func() (int, error, error) {
			primaryPod, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			out, stdErr, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   primaryPod.GetName(),
				},
				"postgres",
				fmt.Sprintf("SELECT count(*) from pg_stat_replication WHERE sync_state = '%s'", syncState))
			Expect(stdErr).To(BeEmpty())
			Expect(err).ShouldNot(HaveOccurred())

			value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
			return value, err, atoiErr
		}, RetryTimeout).Should(BeEquivalentTo(expectedCount))
	}

	compareSynchronousStandbyNames := func(namespace, clusterName, element string) {
		Eventually(func() string {
			primaryPod, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			out, stdErr, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   primaryPod.GetName(),
				},
				"postgres",
				"select setting from pg_settings where name = 'synchronous_standby_names'")
			Expect(stdErr).To(BeEmpty())
			Expect(err).ShouldNot(HaveOccurred())

			return strings.Trim(out, "\n")
		}, 30).Should(ContainSubstring(element))
	}

	Context("Legacy synchronous replication", func() {
		var namespace string

		It("can manage sync replicas", func() {
			const (
				namespacePrefix = "legacy-sync-replicas-e2e"
				sampleFile      = fixturesDir + "/sync_replicas/cluster-sync-replica-legacy.yaml.template"
			)
			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			// First we check that the starting situation is the expected one
			By("checking that we have the correct amount of sync replicas", func() {
				// We should have 2 candidates for quorum standbys
				getSyncReplicationCount(namespace, clusterName, "quorum", 2)
			})
			By("checking that synchronous_standby_names reflects cluster's changes", func() {
				// Set MaxSyncReplicas to 1
				Eventually(func(g Gomega) error {
					cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())

					cluster.Spec.MaxSyncReplicas = 1
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(BeNil())

				// Scale the cluster down to 2 pods
				_, _, err := run.Run(fmt.Sprintf("kubectl scale --replicas=2 -n %v cluster/%v", namespace, clusterName))
				Expect(err).ToNot(HaveOccurred())
				timeout := 120
				// Wait for pod 3 to be completely terminated
				Eventually(func() (int, error) {
					podList, err := clusterutils.GetClusterPodList(env.Ctx, env.Client, namespace, clusterName)
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(2))

				// We should now only have 1 candidate for quorum replicas
				getSyncReplicationCount(namespace, clusterName, "quorum", 1)
				compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
			})
			By("failing when SyncReplicas fields are invalid", func() {
				cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				// Expect an error. MaxSyncReplicas must be lower than the number of instances
				cluster.Spec.MaxSyncReplicas = 2
				err = env.Client.Update(env.Ctx, cluster)
				Expect(err).To(HaveOccurred())

				cluster, err = clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				// Expect an error. MinSyncReplicas must be lower than MaxSyncReplicas
				cluster.Spec.MinSyncReplicas = 2
				err = env.Client.Update(env.Ctx, cluster)
				Expect(err).To(HaveOccurred())
			})
		})

		It("will not prevent a cluster with pg_stat_statements from being created", func() {
			const (
				namespacePrefix = "sync-replicas-statstatements"
				sampleFile      = fixturesDir + "/sync_replicas/cluster-pgstatstatements.yaml.template"
			)
			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Are extensions a problem with synchronous replication? No, absolutely not,
			// but to install pg_stat_statements you need to create the relative extension
			// and that will be done just after having bootstrapped the first instance,
			// which is the primary.
			// If the number of ready replicas is not taken into consideration while
			// bootstrapping the cluster, the CREATE EXTENSION instruction will block
			// the primary since the desired number of synchronous replicas (even when 1)
			// is not met.
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			AssertClusterIsReady(namespace, clusterName, 30, env)

			By("checking that have 2 quorum-based replicas", func() {
				getSyncReplicationCount(namespace, clusterName, "quorum", 2)
				compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
			})
		})
	})

	Context("Synchronous replication", func() {
		var namespace string

		It("can manage quorum/priority based synchronous replication", func() {
			const (
				namespacePrefix = "sync-replicas-e2e"
				sampleFile      = fixturesDir + "/sync_replicas/cluster-sync-replica.yaml.template"
			)
			clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
			Expect(err).ToNot(HaveOccurred())

			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)

			By("verifying we have 2 quorum-based replicas", func() {
				getSyncReplicationCount(namespace, clusterName, "quorum", 2)
				compareSynchronousStandbyNames(namespace, clusterName, "ANY 2")
			})

			By("setting MaxStandbyNamesFromCluster to 1 and decreasing to 1 the sync replicas required", func() {
				Eventually(func(g Gomega) error {
					cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.Synchronous.MaxStandbyNamesFromCluster = ptr.To(1)
					cluster.Spec.PostgresConfiguration.Synchronous.Number = 1
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(BeNil())

				getSyncReplicationCount(namespace, clusterName, "quorum", 1)
				compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
			})

			By("switching to MethodFirst (priority-based)", func() {
				Eventually(func(g Gomega) error {
					cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.Synchronous.Method = apiv1.SynchronousReplicaConfigurationMethodFirst
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(BeNil())

				getSyncReplicationCount(namespace, clusterName, "sync", 1)
				compareSynchronousStandbyNames(namespace, clusterName, "FIRST 1")
			})

			By("by properly setting standbyNamesPre and standbyNamesPost", func() {
				Eventually(func(g Gomega) error {
					cluster, err := clusterutils.GetCluster(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.Synchronous.MaxStandbyNamesFromCluster = nil
					cluster.Spec.PostgresConfiguration.Synchronous.StandbyNamesPre = []string{"preSyncReplica"}
					cluster.Spec.PostgresConfiguration.Synchronous.StandbyNamesPost = []string{"postSyncReplica"}
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(BeNil())
				compareSynchronousStandbyNames(namespace, clusterName, "FIRST 1 (\"preSyncReplica\"")
				compareSynchronousStandbyNames(namespace, clusterName, "\"postSyncReplica\")")
			})
		})
	})
})
