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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/fencing"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/logs"
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
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
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
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
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
		}, 60).Should(ContainSubstring(element))
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
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())

					cluster.Spec.MaxSyncReplicas = 1
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(Succeed())

				// Scale the cluster down to 2 pods
				_, _, err := run.Run(fmt.Sprintf("kubectl scale --replicas=2 -n %v cluster/%v", namespace,
					clusterName))
				Expect(err).ToNot(HaveOccurred())
				timeout := 120
				// Wait for pod 3 to be completely terminated
				Eventually(func() (int, error) {
					podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					return len(podList.Items), err
				}, timeout).Should(BeEquivalentTo(2))

				// We should now only have 1 candidate for quorum replicas
				getSyncReplicationCount(namespace, clusterName, "quorum", 1)
				compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
			})
			By("failing when SyncReplicas fields are invalid", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				// Expect an error. MaxSyncReplicas must be lower than the number of instances
				cluster.Spec.MaxSyncReplicas = 2
				err = env.Client.Update(env.Ctx, cluster)
				Expect(err).To(HaveOccurred())

				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
		It("can manage quorum/priority based synchronous replication", func() {
			var namespace string
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
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.Synchronous.MaxStandbyNamesFromCluster = ptr.To(1)
					cluster.Spec.PostgresConfiguration.Synchronous.Number = 1
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(Succeed())

				getSyncReplicationCount(namespace, clusterName, "quorum", 1)
				compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
			})

			By("switching to MethodFirst (priority-based)", func() {
				Eventually(func(g Gomega) error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.Synchronous.Method = apiv1.SynchronousReplicaConfigurationMethodFirst
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(Succeed())

				getSyncReplicationCount(namespace, clusterName, "sync", 1)
				compareSynchronousStandbyNames(namespace, clusterName, "FIRST 1")
			})

			By("by properly setting standbyNamesPre and standbyNamesPost", func() {
				Eventually(func(g Gomega) error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					cluster.Spec.PostgresConfiguration.Synchronous.MaxStandbyNamesFromCluster = nil
					cluster.Spec.PostgresConfiguration.Synchronous.StandbyNamesPre = []string{"preSyncReplica"}
					cluster.Spec.PostgresConfiguration.Synchronous.StandbyNamesPost = []string{"postSyncReplica"}
					return env.Client.Update(env.Ctx, cluster)
				}, RetryTimeout, 5).Should(Succeed())
				compareSynchronousStandbyNames(namespace, clusterName, "FIRST 1 (\"preSyncReplica\"")
				compareSynchronousStandbyNames(namespace, clusterName, "\"postSyncReplica\")")
			})
		})

		Context("data durability is preferred", func() {
			It("will decrease the number of sync replicas to the number of available replicas", func() {
				var namespace string
				const (
					namespacePrefix = "sync-replicas-preferred"
					sampleFile      = fixturesDir + "/sync_replicas/preferred.yaml.template"
				)
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
				Expect(err).ToNot(HaveOccurred())

				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFile, env)

				By("verifying we have 2 quorum-based replicas", func() {
					getSyncReplicationCount(namespace, clusterName, "quorum", 2)
					compareSynchronousStandbyNames(namespace, clusterName, "ANY 2")
				})

				By("fencing a replica and verifying we have only 1 quorum-based replica", func() {
					Expect(fencing.On(env.Ctx, env.Client, fmt.Sprintf("%v-3", clusterName),
						namespace, clusterName, fencing.UsingAnnotation)).Should(Succeed())
					getSyncReplicationCount(namespace, clusterName, "quorum", 1)
					compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
				})
				By("fencing the second replica and verifying we unset synchronous_standby_names", func() {
					Expect(fencing.On(env.Ctx, env.Client, fmt.Sprintf("%v-2", clusterName),
						namespace, clusterName, fencing.UsingAnnotation)).Should(Succeed())
					Eventually(func() string {
						commandTimeout := time.Second * 10
						primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
						Expect(err).ToNot(HaveOccurred())

						stdout, _, err := exec.Command(
							env.Ctx, env.Interface, env.RestClientConfig,
							*primary, specs.PostgresContainerName, &commandTimeout,
							"psql", "-U", "postgres", "-tAc", "show synchronous_standby_names",
						)
						Expect(err).ToNot(HaveOccurred())
						return strings.Trim(stdout, "\n")
					}, 160).Should(BeEmpty())
				})
				By("unfencing the replicas and verifying we have 2 quorum-based replicas", func() {
					Expect(fencing.Off(env.Ctx, env.Client, fmt.Sprintf("%v-3", clusterName),
						namespace, clusterName, fencing.UsingAnnotation)).Should(Succeed())
					Expect(fencing.Off(env.Ctx, env.Client, fmt.Sprintf("%v-2", clusterName),
						namespace, clusterName, fencing.UsingAnnotation)).Should(Succeed())
					getSyncReplicationCount(namespace, clusterName, "quorum", 2)
					compareSynchronousStandbyNames(namespace, clusterName, "ANY 2")
				})
			})
		})

		Context("Lag-control in startup probe", func() {
			It("lag control in startup probe will delay the readiness of replicas", func() {
				var namespace string
				const (
					namespacePrefix = "sync-replicas-preferred"
					sampleFile      = fixturesDir + "/sync_replicas/lagcontrol.yaml.template"
				)
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
				Expect(err).ToNot(HaveOccurred())

				fencedReplicaName := fmt.Sprintf("%s-2", clusterName)

				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFile, env)

				By("verifying we have 2 quorum-based replicas", func() {
					getSyncReplicationCount(namespace, clusterName, "quorum", 2)
					compareSynchronousStandbyNames(namespace, clusterName, "ANY 2")
				})

				By("fencing a replica and verifying we have only 1 quorum-based replica", func() {
					Expect(fencing.On(env.Ctx, env.Client, fencedReplicaName,
						namespace, clusterName, fencing.UsingAnnotation)).Should(Succeed())
					getSyncReplicationCount(namespace, clusterName, "quorum", 1)
					compareSynchronousStandbyNames(namespace, clusterName, "ANY 1")
				})

				By("waiting for the fenced pod to be not ready", func() {
					Eventually(func(g Gomega) bool {
						var pod corev1.Pod
						err := env.Client.Get(env.Ctx, client.ObjectKey{
							Namespace: namespace,
							Name:      fencedReplicaName,
						}, &pod)
						g.Expect(err).ToNot(HaveOccurred())

						return utils.IsPodReady(pod)
					}, 160).Should(BeFalse())
				})

				By("adding data to the primary", func() {
					commandTimeout := time.Second * 600
					primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					// This will generate 1Gi of data in the primary node and, since the replica we fenced
					// is not aligned, will generate lag.
					_, _, err = exec.Command(
						env.Ctx, env.Interface, env.RestClientConfig,
						*primary, specs.PostgresContainerName, &commandTimeout,
						"psql",
						"-U",
						"postgres",
						"-c",
						"create table numbers (i integer); "+
							"insert into numbers (select generate_series(1,1000000)); "+
							"insert into numbers (select * from numbers); "+
							"insert into numbers (select * from numbers); "+
							"insert into numbers (select * from numbers); ",
					)
					Expect(err).ToNot(HaveOccurred())
				})

				By("stopping the reconciliation loop on the cluster", func() {
					// This is needed to avoid the operator to recreate the new Pod when we'll
					// delete it.
					// We want the Pod to start without being fenced to engage the lag checking
					// startup probe
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					origCluster := cluster.DeepCopy()
					if cluster.Annotations == nil {
						cluster.Annotations = make(map[string]string)
					}
					cluster.Annotations[utils.ReconciliationLoopAnnotationName] = "disabled"

					err = env.Client.Patch(env.Ctx, cluster, client.MergeFrom(origCluster))
					Expect(err).ToNot(HaveOccurred())
				})

				By("deleting the test replica and disabling fencing", func() {
					var pod corev1.Pod
					err := env.Client.Get(env.Ctx, client.ObjectKey{
						Namespace: namespace,
						Name:      fencedReplicaName,
					}, &pod)
					Expect(err).ToNot(HaveOccurred())

					err = env.Client.Delete(env.Ctx, &pod)
					Expect(err).ToNot(HaveOccurred())

					Expect(fencing.Off(env.Ctx, env.Client, fmt.Sprintf("%v-2", clusterName),
						namespace, clusterName, fencing.UsingAnnotation)).Should(Succeed())
				})

				By("enabling the reconciliation loops on the cluster", func() {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					origCluster := cluster.DeepCopy()
					if cluster.Annotations == nil {
						cluster.Annotations = make(map[string]string)
					}
					delete(cluster.Annotations, utils.ReconciliationLoopAnnotationName)

					err = env.Client.Patch(env.Ctx, cluster, client.MergeFrom(origCluster))
					Expect(err).ToNot(HaveOccurred())
				})

				By("waiting for the replica to be back again and ready", func() {
					Eventually(func(g Gomega) bool {
						var pod corev1.Pod
						err := env.Client.Get(env.Ctx, client.ObjectKey{
							Namespace: namespace,
							Name:      fencedReplicaName,
						}, &pod)
						g.Expect(err).ToNot(HaveOccurred())

						return utils.IsPodReady(pod)
					}, 160).Should(BeTrue())
				})

				By("checking that the replica was waiting for the lag to decrease before being ready", func() {
					timeout := 2 * time.Minute

					// This "Eventually" block is needed because we may grab only a portion
					// of the replica logs, and the "ParseJSONLogs" function may fail on the latest
					// log record when this happens
					Eventually(func(g Gomega) {
						data, err := logs.ParseJSONLogs(env.Ctx, env.Interface, namespace, fencedReplicaName)
						g.Expect(err).ToNot(HaveOccurred())

						recordWasFound := false
						for _, record := range data {
							err, ok := record["err"].(string)
							if !ok {
								continue
							}

							if strings.Contains(err, "streaming replica lagging") {
								recordWasFound = true
								break
							}
						}

						g.Expect(recordWasFound).To(
							BeTrue(),
							"The startup probe is preventing the replica from being marked ready",
						)
					}, timeout).Should(Succeed())
				})
			})
		})
	})
})
